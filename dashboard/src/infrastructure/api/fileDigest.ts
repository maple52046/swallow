import { createSHA256 } from 'hash-wasm'

/**
 * Bytes read from the file per hashing step. Large enough that a multi-gigabyte image hashes in
 * few steps, small enough that each step's buffer is a bounded allocation and the page stays
 * responsive between steps.
 */
const HASH_CHUNK_BYTES = 8 * 1024 * 1024

/**
 * Computes the lowercase hex SHA-256 of a file the user selected, for the OS image upload intent
 * (`sha256` of `POST /api/v1/provisioning/image-uploads`, which the provisioner verifies the
 * received content against).
 *
 * The file is read in {@link HASH_CHUNK_BYTES} slices and fed to an incremental hasher, because
 * WebCrypto's `digest` needs the whole input in memory and an image can be many gigabytes. Each
 * slice is awaited, so the page keeps rendering between steps; `onProgress` receives the number of
 * bytes hashed so far after each one. A read failure (the file was removed or changed on disk)
 * rejects with the browser's error.
 */
export async function sha256OfFile(file: Blob, onProgress?: (hashedBytes: number) => void): Promise<string> {
  const hasher = await createSHA256()
  hasher.init()
  for (let offset = 0; offset < file.size; offset += HASH_CHUNK_BYTES) {
    const end = Math.min(offset + HASH_CHUNK_BYTES, file.size)
    hasher.update(new Uint8Array(await file.slice(offset, end).arrayBuffer()))
    onProgress?.(end)
  }
  return hasher.digest('hex')
}
