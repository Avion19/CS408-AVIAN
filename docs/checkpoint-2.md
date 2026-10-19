# Checkpoint 2: shared site, accounts, and deployment

This is preparation for the course checkpoint. No discussion post, video, or peer reviews are submitted by these files.

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
| Deploy to EC2 | Pending verification | Public release and revision must be checked after tagging. |
| Demonstrate Nginx and service permissions | Pending verification | Check proxy response and `systemctl show`, directory owners and modes on EC2. |
| Startup after reboot | Pending verification | Reboot the instance; check changed boot ID, active/enabled services, persistent login, and public revision. |

Local automated verification: `go test ./...`, `go vet ./...`, and shell syntax checks pass. Deployment and fresh-clone evidence are filled in after the tagged release is verified.

## Demo recording outline (under five minutes)

Record in Panopto inside your course reply. Keep the EC2 address bar visible.

1. **0:00–1:15:** Open the live Home. Show the original bird illustration, field-journal palette, species previews, public chirp/empty state, shared header/footer, and navigation links. Explain that features scheduled later have page shells.
2. **1:15–2:30:** Follow Write a Chirp while signed out. Show the Login/Register tabs, register an account, and demonstrate returning to the requested page with your username in the header. Log out, show a failed login, then log in successfully. Do not expose passwords or secrets.
3. **2:30–3:30:** Open the cp2 source on GitHub. Briefly show bcrypt and token hashing in `accounts.go`, CSRF verification in `server.go`, and the passing account test results.
4. **3:30–4:30:** Show the verified EC2 service user/permissions, enabled Nginx and Avian services, boot verification evidence, and the live health header matching the cp2 commit. Finish with the public site after reboot.

## Submission preparation

After the release is verified, use the prepared submission draft with the exact output from `git diff --shortstat cp1 cp2`. Replace its Spec placeholder with your approved Google Doc URL. Verify the GitHub Tag link while signed out. Record the Panopto video yourself; no video is claimed by this repository. Peer reviews require two actual classmates' posts and remain a separate course activity.
