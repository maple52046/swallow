// swallow-gpu-probe: the GPU check a Slurm deployment's gate runs in a container (decision 083).
//
// It sees only the GPU devices the job was given (the container gets /dev/kfd and the job's render
// nodes) and, on each of them, allocates memory, runs a vector-add kernel, copies the result back,
// and checks it, --runs times. The kernel is built for every Instinct gfx target the image's ROCm
// supports, so the host needs only the amdgpu driver. It prints one JSON line:
//   {"ok": bool, "devices": N, "expected": N, "gfx": [...], "error": "..."}
// and exits 0 only when ok. A run that expects no GPU passes when the container sees none.
#include <hip/hip_runtime.h>

#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <string>
#include <vector>

__global__ void add(const float* a, const float* b, float* c, int n) {
  int i = blockIdx.x * blockDim.x + threadIdx.x;
  if (i < n) c[i] = a[i] + b[i];
}

static std::string escape(const std::string& text) {
  std::string out;
  for (char ch : text) {
    if (ch == '"' || ch == '\\') out.push_back('\\');
    if (ch == '\n') { out += "\\n"; continue; }
    out.push_back(ch);
  }
  return out;
}

static int report(bool ok, int devices, int expected, const std::vector<std::string>& gfx, const std::string& error) {
  std::printf("{\"ok\": %s, \"devices\": %d, \"expected\": %d, \"gfx\": [", ok ? "true" : "false", devices, expected);
  for (size_t i = 0; i < gfx.size(); ++i) std::printf("%s\"%s\"", i ? ", " : "", escape(gfx[i]).c_str());
  std::printf("], \"error\": \"%s\"}\n", escape(error).c_str());
  return ok ? 0 : 1;
}

int main(int argc, char** argv) {
  int expected = -1, runs = 1;
  for (int i = 1; i < argc; ++i) {
    if (std::strncmp(argv[i], "--expect=", 9) == 0) expected = std::atoi(argv[i] + 9);
    if (std::strncmp(argv[i], "--runs=", 7) == 0) runs = std::atoi(argv[i] + 7);
  }
  int devices = 0;
  hipError_t status = hipGetDeviceCount(&devices);
  if (status == hipErrorNoDevice) devices = 0;
  else if (status != hipSuccess) return report(false, 0, expected, {}, std::string("hipGetDeviceCount: ") + hipGetErrorString(status));
  std::vector<std::string> gfx;
  if (expected >= 0 && devices != expected) {
    return report(false, devices, expected, gfx, "the container sees " + std::to_string(devices) + " GPUs, want " + std::to_string(expected));
  }
  const int n = 1 << 20;
  std::vector<float> a(n, 1.5f), b(n, 2.25f), c(n);
  for (int device = 0; device < devices; ++device) {
    hipDeviceProp_t props;
    if (hipGetDeviceProperties(&props, device) == hipSuccess) gfx.push_back(props.gcnArchName);
    for (int run = 0; run < runs; ++run) {
      float *da = nullptr, *db = nullptr, *dc = nullptr;
      const size_t bytes = n * sizeof(float);
      std::string step;
      auto fail = [&](hipError_t err) {
        return report(false, devices, expected, gfx, "device " + std::to_string(device) + " " + step + ": " + hipGetErrorString(err));
      };
      if ((status = hipSetDevice(device)) != hipSuccess) { step = "hipSetDevice"; return fail(status); }
      step = "hipMalloc";
      if ((status = hipMalloc(&da, bytes)) != hipSuccess || (status = hipMalloc(&db, bytes)) != hipSuccess ||
          (status = hipMalloc(&dc, bytes)) != hipSuccess) return fail(status);
      step = "hipMemcpy";
      if ((status = hipMemcpy(da, a.data(), bytes, hipMemcpyHostToDevice)) != hipSuccess ||
          (status = hipMemcpy(db, b.data(), bytes, hipMemcpyHostToDevice)) != hipSuccess) return fail(status);
      step = "kernel";
      hipLaunchKernelGGL(add, dim3((n + 255) / 256), dim3(256), 0, 0, da, db, dc, n);
      if ((status = hipGetLastError()) != hipSuccess || (status = hipDeviceSynchronize()) != hipSuccess) return fail(status);
      step = "hipMemcpy back";
      if ((status = hipMemcpy(c.data(), dc, bytes, hipMemcpyDeviceToHost)) != hipSuccess) return fail(status);
      for (int i = 0; i < n; i += 4099) {
        if (c[i] != 3.75f) return report(false, devices, expected, gfx, "device " + std::to_string(device) + " computed a wrong sum");
      }
      hipFree(da);
      hipFree(db);
      hipFree(dc);
    }
  }
  return report(true, devices, expected, gfx, "");
}
