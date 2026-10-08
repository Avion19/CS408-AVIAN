# GoTH Hello World

A small Go app that serves a styled Hello World landing page. It uses Echo for
HTTP routing, Templ for server-side rendering, and the committed Tailwind CSS
stylesheet in `static/css/output.css`.

## Local setup

Prerequisites: Linux, Git, and Go 1.22 or newer. Install Go before cloning the
repository. The generated Templ source and Tailwind stylesheet are included,
so running the app does not require Node.js or separate frontend tools.

From the repository root, run:

```bash
./start.sh
```

The script checks the Go version, downloads Go module dependencies, and starts
the app at <http://localhost:8080/>. Open that URL in a browser. Set `PORT` to
use another local port, for example `PORT=8081 ./start.sh`. The health route
`/api/health` responds with HTTP 200 and an empty body.

## EC2 setup and deployment

These commands use the current EC2 hostname and key path. Run laptop commands
from the repository root. The `-h` argument to the deploy script is only the
hostname; the script supplies the `ubuntu@` login itself. Keep the `.pem` key
outside this repository.

Deployment also requires the OpenSSH client (`ssh` and `scp`), `rsync`, and
`curl` on your laptop. On Debian or Ubuntu, install them with:

```bash
sudo apt update
sudo apt install openssh-client rsync curl
```

The EC2 security group must allow inbound SSH (TCP 22) from your address and
HTTP (TCP 80). Port 8080 is used by the app behind nginx and does not need to
be open to the internet.

### One-time server setup

From your laptop, copy the deployment files:

```bash
scp -i "$HOME/cs408/aws/awscs408key.pem" -r deploy ubuntu@ec2-52-89-244-87.us-west-2.compute.amazonaws.com:~
```

Connect to the EC2 server:

```bash
ssh -i "$HOME/cs408/aws/awscs408key.pem" ubuntu@ec2-52-89-244-87.us-west-2.compute.amazonaws.com
```

Run setup on the server, then exit back to your laptop:

```bash
sudo bash deploy/setup-ec2.sh
exit
```

The setup script installs Go and nginx, creates `/opt/goth-hello-world`,
configures nginx to forward HTTP requests to the app, and enables the
`goth-hello-world.service` systemd unit to start at boot and restart after a
failure.

### Deploy the app

From your laptop, in the repository root, run:

```bash
./deploy/deploy.sh -h ec2-52-89-244-87.us-west-2.compute.amazonaws.com -i "$HOME/cs408/aws/awscs408key.pem"
```

The script runs the Go tests, copies the project to EC2 with rsync, downloads
the Go module dependencies there, builds the app, restarts the systemd
service, and checks `/api/health`. When it reports success, open
<http://ec2-52-89-244-87.us-west-2.compute.amazonaws.com/>.

To verify startup after reboot, run this from your laptop, wait for EC2 to
come back online, and load the site again:

```bash
ssh -i "$HOME/cs408/aws/awscs408key.pem" ubuntu@ec2-52-89-244-87.us-west-2.compute.amazonaws.com 'sudo reboot'
```

Rebooting keeps the public IP. Stopping and starting the instance can change
its public IP and hostname; update the hostname in these commands if that
happens.

## Project files

- `start.sh` checks Go, installs project dependencies, and starts the app.
- `deploy/setup-ec2.sh` prepares the EC2 server.
- `deploy/goth-hello-world.service` defines the systemd service.
- `deploy/deploy.sh` tests and deploys the app from your laptop.
- `main.go` defines the home page and health routes.
- `views/home.templ` and `views/home_templ.go` define the landing page.
- `static/css/output.css` is the stylesheet served to browsers.

Never commit a `.pem` key or `.env` file. Both are excluded by `.gitignore`,
and the deploy script excludes them when copying the project to EC2.

## Development

To edit the page, update `views/home.templ` and regenerate the Go source with
the Templ CLI:

```bash
go install github.com/a-h/templ/cmd/templ@v0.2.793
$(go env GOPATH)/bin/templ generate
```

To rebuild Tailwind CSS, provide the standalone Tailwind CLI at
`.tools/tailwindcss` and run `make css`. `make build` regenerates both
generated assets; `make test` regenerates Templ source and runs the Go tests.
