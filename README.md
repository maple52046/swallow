# swallow

Data Center API Service — backend component of the GDCM platform.

## Commands

```
swallow api     Start the HTTP API server
swallow agent   Start the agent (not yet implemented)
```

## Running the API server

### With environment variables (quickstart)

```bash
export GDCM_API_MONGO_URI="mongodb://localhost:27017"
export GDCM_API_MONGO_DB="swallow"
export GDCM_API_JWT_SECRET="your-secret"

swallow api
```

### With a config file

```bash
cp docs/config-example.yaml swallow.yaml
# edit swallow.yaml as needed
swallow api --config swallow.yaml
```

### With CLI flags

```bash
swallow api --addr :8080 --mongo-uri mongodb://localhost:27017 --mongo-db swallow
```

## Configuration priority (highest → lowest)

1. Environment variables (`GDCM_API_*`, `GDCM_AGENT_*`)
2. CLI flags (`--addr`, `--mongo-uri`, …)
3. Config file (`--config path/to/swallow.yaml`)
4. Default values

## Environment variables

| Variable                             | Config field                    | Default                     |
|--------------------------------------|---------------------------------|-----------------------------|
| `GDCM_API_ADDR`                      | `api.addr`                      | `:3000`                     |
| `GDCM_API_MONGO_URI`                 | `api.mongoUri`                  | `mongodb://localhost:27017` |
| `GDCM_API_MONGO_DB`                  | `api.mongoDb`                   | `swallow`                   |
| `GDCM_API_JWT_SECRET`                | `api.jwtSecret`                 | *(required in prod)*        |
| `GDCM_API_JWT_EXPIRY_HOURS`          | `api.jwtExpiryHours`            | `24`                        |
| `GDCM_API_BOOTSTRAP_ADMIN_USERNAME`  | `api.bootstrapAdminUsername`    | `admin`                     |
| `GDCM_API_BOOTSTRAP_ADMIN_PASSWORD`  | `api.bootstrapAdminPassword`    | `admin`                     |
| `GDCM_AGENT_CONTROLLER_ADDR`         | `agent.controllerAddr`          | `http://localhost:3000`     |

Legacy variable names (`MONGO_URI`, `MONGO_DB`, `JWT_SECRET`, etc.) are still accepted
with a deprecation warning. Migrate to `GDCM_`-prefixed names when convenient.

## Building

```bash
# canonical binary (supports subcommands)
go build -o bin/swallow ./cmd/swallow

# legacy entry point (backward compat, API mode only)
go build -o bin/swallow-api ./cmd/api
```

## Config file reference

See [`docs/config-example.yaml`](docs/config-example.yaml) for an annotated example.
