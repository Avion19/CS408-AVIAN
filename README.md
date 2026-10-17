# Avian

Avian is a birdwatching community application for CS 408. Checkpoint 1 establishes the Go/Echo server, a versioned SQLite schema, repeatable species data, and a Home page that reads that data. Account, chirp, flock, and search features follow in later checkpoints.

## Run from a fresh clone

The supported development environment is Ubuntu LTS or Ubuntu under WSL. After cloning, run this from the repository root:

```bash
./start.sh
```

Open <http://localhost:8080/>. Stop the foreground server with Ctrl+C. Run the same command again to restart it; the database and local configuration are retained. This implementation was checked on Ubuntu 24.04.4 LTS with Go 1.22.2.

`start.sh` uses a system Go 1.22 or newer when available. Otherwise it downloads Go 1.22.2 into the ignored `.tools/` directory. On Ubuntu, it installs `curl` and `tar` with `apt-get` if needed. It then downloads Go modules, creates private local configuration, builds Avian, and launches it. The executable applies pending migrations and inserts missing species before serving requests. A fresh clone needs network access for Go and module downloads.

## Configuration

| Variable | Local default | Purpose |
| --- | --- | --- |
| `PORT` | `8080` | HTTP port when `LISTEN_ADDR` is unset. |
| `LISTEN_ADDR` | `:<PORT>` | Full listen address; EC2 uses `127.0.0.1:8080` behind Nginx. |
| `DATABASE_PATH` | `./data/avian.db` | SQLite database file. |
| `UPLOAD_DIR` | `./data/uploads` | Reserved persistent directory for later photo uploads. |
| `SESSION_SECRET` | Random value in `.env` | Reserved for Checkpoint 2 sessions. |

Set variables in the shell or edit the local `.env` file. On first run, `start.sh` creates `.env` with a random secret and file mode 600. `.env`, `data/`, and `.tools/` are excluded from Git. Keep any SSH private key outside the repository.

## Database and request flow

`main.go` reads configuration, prepares writable directories, opens SQLite, runs `Store.Initialize`, then starts Echo. The app enables foreign keys and a busy timeout on its SQLite connection. `Store.Initialize` applies embedded SQL files from `migrations/` in filename order. Each migration and its `schema_migrations` record commit in one transaction. It then inserts catalog species that are missing by common name, so restarting the app does not duplicate them.

The first migration defines eight core tables:

| Table | Role |
| --- | --- |
| `users`, `members`, `sessions` | Login credentials, public profiles, and sessions. |
| `species` | Seeded bird reference catalog. |
| `flocks`, `flock_members` | Groups, membership, and ownership roles. |
| `chirps`, `chirp_species` | Posts and their species tags. |

Foreign keys, uniqueness rules, visibility and role checks, and query indexes are in `migrations/001_initial.sql`. Application transactions in later checkpoints will enforce rules that span rows, such as retaining one flock owner and deleting flock-only posts before a flock is removed.

`GET /` queries SQLite for species on each request and renders them with Go `html/template`. The current Home page is the Checkpoint 1 foundation; the full Home design, shared navigation, and recent chirps arrive later. `GET /api/health` returns HTTP 200 when the database connection is available. The catalog currently has 12 project-written species summaries. Each card links to the Cornell Lab All About Birds account for that species. Reference images and their license records arrive with the Species page in Checkpoint 4.

## EC2 setup and deployment

Use an Ubuntu LTS instance with inbound HTTP on port 80 and SSH restricted to your address. The instance must be reachable from the machine running `deploy/deploy.sh`. Install SSH, SCP, rsync, and curl on that machine. The deploy script accepts a system Go or the local toolchain installed by `./start.sh`.

Copy the `deploy/` directory to the instance, then run the one-time setup there:

```bash
sudo bash deploy/setup-ec2.sh
```

The setup creates an unprivileged `avian` account, `/opt/avian` for the release, `/var/lib/avian` for the database and uploads, and `/etc/avian/avian.env` for protected configuration. It installs and enables the `avian` systemd service and Nginx. Nginx forwards public port 80 to Avian on `127.0.0.1:8080`. Repeating setup preserves the environment file.

From the local repository root, deploy with:

```bash
EC2_HOST=<public-hostname> SSH_KEY_PATH=<path-to-key.pem> bash deploy/deploy.sh
```

`EC2_USER` defaults to `ubuntu`. The deploy script detects the instance CPU architecture, builds a Linux binary, copies the binary and static assets, restarts the service, and checks `http://<public-hostname>/api/health`. It does not replace the database, uploads, or server secret. The scripts are prepared for Checkpoint 1's live-site requirement; their EC2 behavior still needs a deployment check before submission.

Useful server checks:

```bash
sudo systemctl status avian nginx
sudo journalctl -u avian -n 100 --no-pager
curl -i http://localhost/api/health
```

## Local verification

`go test ./...` checks that a temporary SQLite database receives the eight tables, repeated initialization leaves the species count unchanged, and the Home route displays seeded data. `bash -n start.sh deploy/setup-ec2.sh deploy/deploy.sh` checks shell syntax.
