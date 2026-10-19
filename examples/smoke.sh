#!/usr/bin/env bash
# End-to-end smoke test for kkrepo-go.
#
# Usage:
#   BASE=http://localhost:8080 ADMIN_USER=admin ADMIN_PASSWORD=admin123 ./examples/smoke.sh
set -euo pipefail

BASE="${BASE:-http://localhost:8080}"
ADMIN_USER="${ADMIN_USER:-admin}"
ADMIN_PASSWORD="${ADMIN_PASSWORD:-admin123}"
SUFFIX="${SUFFIX:-$(date +%s)}"
COOKIE="$(mktemp)"
WORK="$(mktemp -d)"
trap 'rm -f "$COOKIE"; rm -rf "$WORK"' EXIT

pass() { printf '  \033[32mPASS\033[0m %s\n' "$1"; }
fail() { printf '  \033[31mFAIL\033[0m %s\n' "$1" >&2; exit 1; }
expect_code() {
	local expected="$1" actual="$2" label="$3"
	[[ "$actual" == "$expected" ]] || fail "$label (expected $expected, got $actual)"
	pass "$label"
}
contains() {
	local file="$1" needle="$2" label="$3"
	grep -q -- "$needle" "$file" || fail "$label (missing: $needle)"
	pass "$label"
}

echo "kkrepo-go smoke test: $BASE (suffix $SUFFIX)"

code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/api/health")
expect_code 200 "$code" "health endpoint"

code=$(curl -s -o /dev/null -w '%{http_code}' -c "$COOKIE" \
	-H 'Content-Type: application/json' \
	-d "{\"username\":\"$ADMIN_USER\",\"password\":\"$ADMIN_PASSWORD\"}" \
	"$BASE/api/login")
expect_code 200 "$code" "admin login"

create_repo() {
	local name="$1" recipe="$2"
	local code
	code=$(curl -s -o /dev/null -w '%{http_code}' -b "$COOKIE" -H 'Content-Type: application/json' \
		-d "{\"name\":\"$name\",\"recipe\":\"$recipe\"}" "$BASE/api/repositories")
	expect_code 201 "$code" "create $recipe repository ($name)"
}

delete_repo() {
	curl -s -o /dev/null -b "$COOKIE" -X DELETE "$BASE/api/repositories/$1" || true
}

M="$BASE/repository/e2e-maven-$SUFFIX"
N="$BASE/repository/e2e-npm-$SUFFIX"
P="$BASE/repository/e2e-pypi-$SUFFIX"
H="$BASE/repository/e2e-helm-$SUFFIX"
C="$BASE/repository/e2e-cargo-$SUFFIX"
D="$BASE/repository/e2e-conda-$SUFFIX"
Y="$BASE/repository/e2e-yum-$SUFFIX"
A="$BASE/repository/e2e-apt-$SUFFIX"
L="$BASE/repository/e2e-alpine-$SUFFIX"
U="$BASE/repository/e2e-pub-$SUFFIX"
R="$BASE/repository/e2e-raw-$SUFFIX"

create_repo "e2e-maven-$SUFFIX" "maven-hosted"
create_repo "e2e-npm-$SUFFIX" "npm-hosted"
create_repo "e2e-pypi-$SUFFIX" "pypi-hosted"
create_repo "e2e-helm-$SUFFIX" "helm-hosted"
create_repo "e2e-cargo-$SUFFIX" "cargo-hosted"
create_repo "e2e-conda-$SUFFIX" "conda-hosted"
create_repo "e2e-yum-$SUFFIX" "yum-hosted"
create_repo "e2e-apt-$SUFFIX" "apt-hosted"
create_repo "e2e-alpine-$SUFFIX" "alpine-hosted"
create_repo "e2e-pub-$SUFFIX" "pub-hosted"
create_repo "e2e-raw-$SUFFIX" "raw-hosted"

# Maven: upload a jar and verify generated maven-metadata.xml.
code=$(curl -s -o /dev/null -w '%{http_code}' -u "$ADMIN_USER:$ADMIN_PASSWORD" -X PUT \
	--data-binary 'jar-bytes' "$M/com/acme/demo/1.0.0/demo-1.0.0.jar")
expect_code 201 "$code" "maven upload"
curl -s "$M/com/acme/demo/maven-metadata.xml" -o "$WORK/maven.xml"
contains "$WORK/maven.xml" '<version>1.0.0</version>' "maven generated metadata"

# npm: publish a packument with one tarball attachment.
python3 - "$N" "$WORK/npm.json" <<'PY'
import base64, json, sys
base, out = sys.argv[1], sys.argv[2]
tar = b"npm tarball"
doc = {
    "name": "demo", "dist-tags": {"latest": "1.0.0"},
    "versions": {"1.0.0": {"name": "demo", "version": "1.0.0", "dist": {"tarball": base + "/demo/-/demo-1.0.0.tgz"}}},
    "_attachments": {"demo-1.0.0.tgz": {"content_type": "application/octet-stream", "data": base64.b64encode(tar).decode(), "length": len(tar)}},
}
open(out, "w").write(json.dumps(doc))
PY
code=$(curl -s -o /dev/null -w '%{http_code}' -u "$ADMIN_USER:$ADMIN_PASSWORD" -X PUT \
	-H 'Content-Type: application/json' --data-binary "@$WORK/npm.json" "$N/demo")
expect_code 201 "$code" "npm publish"
curl -s "$N/demo" -o "$WORK/npm-packument.json"
contains "$WORK/npm-packument.json" '"dist-tags"' "npm packument"
code=$(curl -s -o "$WORK/npm.tgz" -w '%{http_code}' "$N/demo/-/demo-1.0.0.tgz")
expect_code 200 "$code" "npm tarball"

# PyPI: multipart upload and simple index.
printf 'sdist' > "$WORK/demo-1.0.0.tar.gz"
code=$(curl -s -o /dev/null -w '%{http_code}' -u "$ADMIN_USER:$ADMIN_PASSWORD" -X POST \
	-F ':action=file_upload' -F 'name=demo' -F 'version=1.0.0' \
	-F "content=@$WORK/demo-1.0.0.tar.gz" "$P/")
expect_code 200 "$code" "pypi upload"
curl -s "$P/simple/demo/" -o "$WORK/pypi.html"
contains "$WORK/pypi.html" 'demo-1.0.0.tar.gz' "pypi simple index"

# Helm: upload a chart and verify index.yaml.
printf 'chart' > "$WORK/demo-chart-1.2.3.tgz"
code=$(curl -s -o /dev/null -w '%{http_code}' -u "$ADMIN_USER:$ADMIN_PASSWORD" -X PUT \
	--data-binary "@$WORK/demo-chart-1.2.3.tgz" "$H/demo-chart-1.2.3.tgz")
expect_code 201 "$code" "helm chart upload"
curl -s "$H/index.yaml" -o "$WORK/helm-index.yaml"
contains "$WORK/helm-index.yaml" 'version: 1.2.3' "helm index"

# Cargo: publish payload (u32LE json length + JSON + u32LE crate length + crate).
python3 - "$C" "$WORK/cargo.bin" <<'PY'
import json, struct, sys
base, out = sys.argv[1], sys.argv[2]
meta = json.dumps({"name": "demo", "vers": "0.1.0", "deps": [], "features": {}}).encode()
crate = b"crate-bytes"
open(out, "wb").write(struct.pack("<I", len(meta)) + meta + struct.pack("<I", len(crate)) + crate)
PY
code=$(curl -s -o /dev/null -w '%{http_code}' -u "$ADMIN_USER:$ADMIN_PASSWORD" -X PUT \
	--data-binary "@$WORK/cargo.bin" "$C/api/v1/crates/new")
expect_code 200 "$code" "cargo publish"
curl -s "$C/de/mo/demo" -o "$WORK/cargo-index.json"
contains "$WORK/cargo-index.json" '"name":"demo"' "cargo sparse index"
code=$(curl -s -o "$WORK/demo.crate" -w '%{http_code}' "$C/api/v1/crates/demo/0.1.0/download")
expect_code 200 "$code" "cargo download"

# Conda: upload a package and verify generated repodata.
code=$(curl -s -o /dev/null -w '%{http_code}' -u "$ADMIN_USER:$ADMIN_PASSWORD" -X PUT \
	--data-binary 'conda' "$D/main/linux-64/demo-1.0.0-0.conda")
expect_code 201 "$code" "conda upload"
curl -s "$D/main/linux-64/repodata.json" -o "$WORK/repodata.json"
contains "$WORK/repodata.json" '"demo-1.0.0-0.conda"' "conda repodata"

# Yum: upload an RPM and verify repomd + primary metadata.
code=$(curl -s -o /dev/null -w '%{http_code}' -u "$ADMIN_USER:$ADMIN_PASSWORD" -X PUT \
	--data-binary 'rpm' "$Y/base/x86_64/demo-1.0.0-1.x86_64.rpm")
expect_code 201 "$code" "yum rpm upload"
curl -s "$Y/base/x86_64/repodata/repomd.xml" -o "$WORK/repomd.xml"
contains "$WORK/repomd.xml" 'repodata/primary.xml.gz' "yum repomd"
curl -s "$Y/base/x86_64/repodata/primary.xml.gz" -o "$WORK/primary.xml.gz"
gzip -dc "$WORK/primary.xml.gz" > "$WORK/primary.xml"
contains "$WORK/primary.xml" '<name>demo</name>' "yum primary metadata"

# APT: upload a deb and verify Packages/Release.
code=$(curl -s -o /dev/null -w '%{http_code}' -u "$ADMIN_USER:$ADMIN_PASSWORD" -X PUT \
	--data-binary 'deb' "$A/pool/main/d/demo/demo_1.0.0_amd64.deb")
expect_code 201 "$code" "apt deb upload"
curl -s "$A/dists/stable/main/binary-amd64/Packages" -o "$WORK/Packages"
contains "$WORK/Packages" 'Package: demo' "apt Packages"
curl -s "$A/dists/stable/Release" -o "$WORK/Release"
contains "$WORK/Release" 'main/binary-amd64/Packages' "apt Release"
curl -s "$A/dists/stable/InRelease" -o "$WORK/InRelease"
contains "$WORK/InRelease" 'BEGIN PGP SIGNED MESSAGE' "apt InRelease signature"
curl -s "$A/dists/stable/Release.gpg" -o "$WORK/Release.gpg"
contains "$WORK/Release.gpg" 'BEGIN PGP SIGNATURE' "apt Release.gpg"
curl -s "$A/gpg.key" -o "$WORK/apt-key.asc"
contains "$WORK/apt-key.asc" 'BEGIN PGP PUBLIC KEY BLOCK' "apt public key"

# Alpine: upload an apk and verify APKINDEX.
code=$(curl -s -o /dev/null -w '%{http_code}' -u "$ADMIN_USER:$ADMIN_PASSWORD" -X PUT \
	--data-binary 'apk' "$L/v3.20/main/x86_64/demo-1.0.0-r0.apk")
expect_code 201 "$code" "alpine apk upload"
curl -s "$L/v3.20/main/x86_64/APKINDEX.tar.gz" -o "$WORK/APKINDEX.tar.gz"
tar -tzf "$WORK/APKINDEX.tar.gz" | grep -q '\.SIGN\.RSA\.' || fail "alpine APKINDEX is not signed"
pass "alpine APKINDEX signature"
tar -xzf "$WORK/APKINDEX.tar.gz" -O APKINDEX > "$WORK/APKINDEX"
contains "$WORK/APKINDEX" 'P:demo' "alpine APKINDEX"
curl -s "$L/alpine.rsa.pub" -o "$WORK/alpine.rsa.pub"
contains "$WORK/alpine.rsa.pub" 'BEGIN PUBLIC KEY' "alpine public key"

# Dart Pub: publish an archive and read package metadata.
mkdir -p "$WORK/pub/lib"
printf 'name: kk_demo_pub\nversion: 1.0.0\ndescription: kkrepo pub demo\n' > "$WORK/pub/pubspec.yaml"
printf 'String hello() => "pub";\n' > "$WORK/pub/lib/kk_demo_pub.dart"
(cd "$WORK/pub" && COPYFILE_DISABLE=1 tar czf "$WORK/pub.tar.gz" .)
PUB_NEW=$(curl -s "$U/api/packages/versions/new")
PUB_URL=$(echo "$PUB_NEW" | python3 -c 'import json,sys; print(json.load(sys.stdin)["url"])')
code=$(curl -s -o /dev/null -w '%{http_code}' -u "$ADMIN_USER:$ADMIN_PASSWORD" -X POST \
	-F "file=@$WORK/pub.tar.gz" "$PUB_URL")
[[ "$code" == "303" ]] || fail "pub upload (expected 303, got $code)"
pass "pub upload"
curl -s "$U/api/packages/kk_demo_pub" -o "$WORK/pub-package.json"
contains "$WORK/pub-package.json" '1.0.0' "pub package metadata"
code=$(curl -s -o "$WORK/pub-download.tar.gz" -w '%{http_code}' "$U/api/archives/kk_demo_pub-1.0.0.tar.gz")
expect_code 200 "$code" "pub archive download"

# Raw + file tree.
code=$(curl -s -o /dev/null -w '%{http_code}' -u "$ADMIN_USER:$ADMIN_PASSWORD" -X PUT \
	--data-binary 'one' "$R/e2e/1.txt")
expect_code 201 "$code" "raw upload 1"
curl -s -o /dev/null -u "$ADMIN_USER:$ADMIN_PASSWORD" -X PUT --data-binary 'two' "$R/e2e/2.txt"
curl -s -o /dev/null -u "$ADMIN_USER:$ADMIN_PASSWORD" -X PUT --data-binary 'three' "$R/e2e/3.txt"
curl -s -b "$COOKIE" "$BASE/api/repositories/e2e-raw-$SUFFIX/browse" -o "$WORK/tree.json"
contains "$WORK/tree.json" '"name":"e2e"' "browse tree root"
curl -s -b "$COOKIE" "$BASE/api/repositories/e2e-raw-$SUFFIX/browse?path=e2e" -o "$WORK/tree-e2e.json"
contains "$WORK/tree-e2e.json" '1.txt' "browse tree directory"

# Content selector: validate expression matching through the API.
code=$(curl -s -o /dev/null -w '%{http_code}' -b "$COOKIE" -H 'Content-Type: application/json' \
	-d "{\"name\":\"e2e-selector-$SUFFIX\",\"expression\":\"path.startsWith(\\\"e2e/\\\")\"}" \
	"$BASE/api/content-selectors")
expect_code 201 "$code" "content selector create"
curl -s -b "$COOKIE" -H 'Content-Type: application/json' \
	-d "{\"expression\":\"path.startsWith(\\\"e2e/\\\")\",\"format\":\"raw\",\"repository\":\"e2e-raw-$SUFFIX\",\"path\":\"e2e/1.txt\"}" \
	"$BASE/api/content-selectors/test" -o "$WORK/selector-test.json"
contains "$WORK/selector-test.json" '"matched":true' "content selector test"
curl -s -o /dev/null -b "$COOKIE" -X DELETE "$BASE/api/content-selectors/e2e-selector-$SUFFIX"

# Cleanup: retain only the two newest files.
code=$(curl -s -o /dev/null -w '%{http_code}' -b "$COOKIE" -H 'Content-Type: application/json' \
	-d "{\"name\":\"e2e-cleanup-$SUFFIX\",\"repository\":\"e2e-raw-$SUFFIX\",\"pattern\":\"e2e/**\",\"retainCount\":2}" \
	"$BASE/api/cleanup/policies")
expect_code 201 "$code" "cleanup policy create"
curl -s -b "$COOKIE" -X POST "$BASE/api/cleanup/policies/e2e-cleanup-$SUFFIX/preview" -o "$WORK/preview.json"
contains "$WORK/preview.json" '"matched":1' "cleanup preview"
curl -s -b "$COOKIE" -X POST "$BASE/api/cleanup/policies/e2e-cleanup-$SUFFIX/run" -o "$WORK/run.json"
contains "$WORK/run.json" '"deleted":1' "cleanup run"

# Backup export and merge import.
curl -s -b "$COOKIE" "$BASE/api/backup/export" -o "$WORK/backup.tgz"
gzip -t "$WORK/backup.tgz" || fail "backup archive is not valid gzip"
pass "backup export"
tar -tzf "$WORK/backup.tgz" | grep -q 'manifest.json' || fail "backup manifest missing"
pass "backup manifest"
curl -s -b "$COOKIE" -X POST --data-binary "@$WORK/backup.tgz" "$BASE/api/backup/import?mode=merge" -o "$WORK/backup-import.json"
contains "$WORK/backup-import.json" '"mode":"merge"' "backup merge import"

# Metrics and audit.
curl -s "$BASE/metrics" -o "$WORK/metrics.txt"
contains "$WORK/metrics.txt" 'kkrepo_http_requests_total' "prometheus metrics"
curl -s -b "$COOKIE" "$BASE/api/audit?action=repository&limit=50" -o "$WORK/audit.json"
contains "$WORK/audit.json" 'repository.create' "audit log"

for name in maven npm pypi helm cargo conda yum apt alpine pub raw; do
	delete_repo "e2e-$name-$SUFFIX"
done
curl -s -o /dev/null -b "$COOKIE" -X DELETE "$BASE/api/cleanup/policies/e2e-cleanup-$SUFFIX"

printf '\n\033[32mAll smoke tests passed.\033[0m\n'
