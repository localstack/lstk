package snowflake

import (
	"net"

	"github.com/localstack/lstk/internal/endpoint"
)

// S3Endpoint returns the value for the Snowflake emulator's SF_S3_ENDPOINT
// variable for the given host port. The Python Snowflake emulator funnels all
// S3 access (including internal stages, e.g. COPY INTO) through this single
// endpoint, so it must match the port the emulator is exposed on; otherwise it
// defaults to 4566 and S3 access fails when the emulator runs on a custom port.
func S3Endpoint(port string) string {
	return "s3." + endpoint.Hostname + ":" + port
}

// NextListenAddr returns the value for the preview Snowflake emulator's
// SNOWFLAKE_LISTEN_ADDR variable, given the port it should serve inside the
// container. The image itself defaults to 8080, but lstk publishes, health-checks
// and advertises every emulator on the LocalStack gateway port, so the listener is
// moved there rather than teaching the rest of lstk a second container port.
func NextListenAddr(containerPort string) string {
	return "0.0.0.0:" + containerPort
}

// NextStateDir is the container path the preview Snowflake emulator declares as a
// VOLUME and puts its embedded PostgreSQL cluster in (the image's PGDATA default
// is the "data" subdirectory of it).
//
// lstk binds a directory over it on every start. That is not about persistence:
// lstk recreates the container each start and `docker rm` leaves an anonymous
// volume behind, so leaving the declaration uncovered would strand a whole
// PostgreSQL cluster's worth of data per start. Covering it also gives --persist
// a place to keep the cluster without overriding PGDATA.
//
// Caveat for --persist on Linux: the emulator runs as a non-root user (uid 1000),
// which has to create the PGDATA subdirectory inside this mount, so a host
// directory owned by a different uid makes PostgreSQL refuse to start. Docker
// Desktop maps ownership and is unaffected. The default (no --persist) path never
// writes here, so only --persist is exposed to it.
const NextStateDir = "/var/lib/snowflake-rs"

// NextEphemeralPGData is where the preview Snowflake emulator's PostgreSQL cluster
// goes when persistence is off. It sits outside NextStateDir, in the container's
// writable layer, so the cluster is discarded with the container — matching what a
// user gets from the other emulators without --persist. The emulator already uses
// /tmp for stages and its TLS cache, so the path is writable by its non-root user.
const NextEphemeralPGData = "/tmp/snowflake-rs/data"

func Hostname(resolvedHost string) string {
	host, _, err := net.SplitHostPort(resolvedHost)
	if err != nil {
		return ""
	}
	if net.ParseIP(host) != nil {
		// Returns "" when resolvedHost is an IP, since prepending a subdomain to an IP is invalid.
		return ""
	}
	return "snowflake." + resolvedHost
}
