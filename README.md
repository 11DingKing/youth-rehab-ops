# Youth Rehab Operations

Youth Rehab Operations is a backend service for handling youth summer-sport injuries from a venue report through professional return-to-training clearance. It keeps operational facts, clinical decisions, guardian communications, scheduling restrictions, overrides, and sensitive-record access separate and auditable.

## Local run

The service requires Go 1.25 or newer and uses SQLite through a pure-Go driver.

```sh
cp .env.example .env
go run ./cmd/server
```

Configuration is read from environment variables. `DB_PATH` defaults to `data/rehab.db`; the parent directory is created on startup. Health endpoints are `GET /healthz` and `GET /readyz`.

The first safety-officer account can be bootstrapped with `BOOTSTRAP_ADMIN_EMAIL` and `BOOTSTRAP_ADMIN_PASSWORD`. Further users are created through the bootstrap service in controlled deployment tooling. Passwords are salted and hashed; opaque session tokens are persisted only as hashes, expire, and are revoked by logout.

## Main workflow

Coaches report incidents but cannot view protected clinical notes or make clearance decisions. Safety officers triage, stop training, coordinate guardian notices, and refer cases. Health professionals accept or return referrals, publish immutable rehabilitation plan versions, record follow-ups, and grant conditional or full clearance. Guardians can view and acknowledge their participant's notices. Scheduling checks the latest non-expired clearance and all active blocks in one transaction.

Every sensitive read and mutation writes a durable audit event. Notification delivery is performed by a restart-safe worker using persisted leases, bounded retries, and permanent-failure records.

## Verification

```sh
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go build ./...
```

The root `Dockerfile` cross-builds the actual `./cmd/server` entry for Linux targets and runs as a non-root user with `/app/data` as the persistent database directory.
The module proxy is a build argument, so restricted networks can use `--build-arg GOPROXY=https://goproxy.cn,direct` without changing dependency versions or checksums.
