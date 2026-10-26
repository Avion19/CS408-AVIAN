# Checkpoint 2: shared site, accounts, and deployment

This document records the Checkpoint 2 deliverables and verification results.

## Approved scope

The approved specification is the local `avian-project-docs/AVIAN_ FINAL DRAFT.md`, under “Project Schedule: Seven Checkpoints.” Its Checkpoint 2 text is:

> Deliver the shared header/footer and all-page navigation, Bootstrap custom palette, finished Home with illustration, Login/Register tabs, sign-in/logout, bcrypt, hashed sessions, and CSRF.
>
> Implement `deploy/setup-ec2.sh` and `deploy/deploy.sh`; deploy to EC2 now and demonstrate Nginx, service permissions, and startup after reboot.

The rows below separate each deliverable from those two paragraphs. Features scheduled for later checkpoints have navigable shared page shells; those future features are not claimed as implemented here.

## Evidence

| Deliverable (Checkpoint 2 in my spec) | Status | Evidence |
| --- | --- | --- |
| Shared header/footer and all-page navigation | Done | `templates/shared.html`; all ten planned page routes in `server.go`; protected links preserve their return destination. |
| Bootstrap custom palette | Done | `static/vendor/bootstrap/bootstrap.min.css`; `static/css/avian.css`. |
| Finished Home with illustration | Done | `templates/home.html`; original `static/images/field-notes.svg`; `accounts.go` reads the three newest public chirps. |
| Login/Register tabs | Done | `/login`, `/login?tab=register`; `templates/login.html`. |
| Sign-in/logout | Done | `POST /register`, `POST /login`, `POST /logout`; `server.go`; `TestAccountLifecycle`. |
| bcrypt password hashing | Done | `accounts.go` uses bcrypt default cost; `accounts_test.go` verifies stored hashes. |
| Hashed sessions | Done | `accounts.go`: random cookie token, SHA-256 hash in SQLite, expiration, persistence and revocation tests. |
| CSRF | Done | `server.go`: signed anonymous tokens and session-specific tokens checked before every state-changing handler; `TestCSRFAndExpiredSessions`. |
| `deploy/setup-ec2.sh` | Done | Sets up Nginx, protected persistent data/config, unprivileged service, and boot enablement. |
| `deploy/deploy.sh` | Done | Requires a clean tagged commit; builds and installs it and verifies the public source-revision header. |
| Deploy to EC2 | Done | Live Home: http://ec2-52-89-244-87.us-west-2.compute.amazonaws.com/ ; `/api/health` reports the deployed source revision. |
| Demonstrate Nginx and service permissions | Done | Public HTTP served by Nginx; app bound to `127.0.0.1:8080`; `User=avian`, `UMask=0077`, `ProtectSystem=strict`; configuration mode 600 and persistent directories mode 700. |
| Startup after reboot | Done | Changed boot ID, both services active/enabled, and successful persisted-session and account-login checks after an actual EC2 reboot on 2026-10-19. |

## Verification performed on 2026-10-19

- `go test ./...`, `go vet ./...`, shell syntax checks, and `git diff --check` passed.
- A fresh clone started with only `./start.sh`, using an empty Go module/build cache and no copied `.env`, database, or `.tools`. The script downloaded dependencies, generated mode-600 configuration, initialized SQLite, and served the app. Live HTTP checks covered Home, local assets, registration, CSRF rejection, logout, and failed/successful login.
- The bundled setup script ran successfully on the existing EC2 instance. Nginx configuration validation passed. Nginx served the public site; Avian listened only on `127.0.0.1:8080`.
- Service inspection showed `User=avian`, `Group=avian`, `UMask=0077`, `ProtectSystem=strict`, `NoNewPrivileges=yes`, and `ReadWritePaths=/var/lib/avian`. The release directory was root-owned with mode 755; persistent directories were owned by `avian` with mode 700; the environment file was root-owned with mode 600.
- The tagged deployment passed live HTTP checks for every shared destination, Bootstrap and illustration assets, protected return paths, registration, cookie attributes, CSRF rejection, logout, failed/successful login, and safe redirect handling.
- An actual reboot changed the boot ID from `30a0df7b-218d-4dbf-b954-49cfe917a944` to `413cf90f-412f-466c-97f1-75c768bd6a1a`. Both services returned active and enabled without manual startup. The existing session remained signed in, the public profile excluded email, and logout plus a new login with the stored account succeeded. The public health response remained HTTP 200 with the expected revision.

The reboot checks used implementation commit `9f72f84`. The final `cp2` commit, `24329e3`, adds documentation; the final public health response was verified against that tagged revision.
