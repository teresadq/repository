# Client compatibility demos

These commands exercise "publish package → resolve/download from another project".
Start kkrepo-go and use `BASE=http://localhost:8080`, admin/admin123 unless noted.

## Go

```bash
# publish a module zip to go-hosted, then consume via go-group
GOPROXY=$BASE/repository/go-group GOSUMDB=off go mod tidy
GOPROXY=$BASE/repository/go-group GOSUMDB=off go run .
```

## npm

```bash
# .npmrc
# registry=http://localhost:8080/repository/npm-hosted/
# //localhost:8080/repository/npm-hosted/:_authToken=<api-token>
npm publish --registry http://localhost:8080/repository/npm-hosted/
npm install kk-demo-npm --registry http://localhost:8080/repository/npm-hosted/
node -e "console.log(require('kk-demo-npm').hello())"
```

## PyPI

```bash
# upload a wheel with multipart (:action=file_upload, name, version, content)
python3 -m venv venv
venv/bin/pip install --index-url $BASE/repository/pypi-hosted/simple \
  --trusted-host localhost --no-deps kk-demo-pypi==1.0.0
```

## Cargo (sparse registry)

```toml
# $CARGO_HOME/config.toml
[registries.kkrepo]
index = "sparse+http://localhost:8080/repository/cargo-hosted/index/"
```

```bash
# $CARGO_HOME/credentials.toml: [registries.kkrepo] token = "<api-token>"
cargo publish --registry kkrepo
# consumer Cargo.toml: kk-demo-cargo = { version = "0.1.0", registry = "kkrepo" }
cargo run
```

## Maven

Upload valid POM/JAR to `maven-hosted`, then:

```bash
mvn -s settings.xml dependency:resolve
# settings.xml: server id kkrepo with admin credentials
# pom.xml repository: http://localhost:8080/repository/maven-hosted
```

## Helm

```bash
helm repo add kkrepo http://localhost:8080/repository/helm-hosted
helm repo update
helm pull kkrepo/kk-demo-helm --version 0.1.0
```

## Swift Package Registry

```bash
swift package-registry set --global --scope kk \
  --allow-insecure-http http://localhost:8080/repository/swift-hosted
# Package.swift dependency: .package(id: "kk.demo", from: "1.0.0")
swift package resolve
swift run
```

## Dart Pub (protocol)

Create a tar.gz containing `pubspec.yaml`, then use
`/api/packages/versions/new` → upload → finalize, and read
`/api/packages/<name>` and `/api/archives/<name>-<version>.tar.gz`.

## Protocol-level formats

Docker, Git LFS, Conda, APT, Alpine, Yum and Raw are covered by
`examples/smoke.sh` (push/upload, index generation, signatures, download).
