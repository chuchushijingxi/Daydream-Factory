#!/bin/sh
set -eu
# Ubuntu 22.04 matches the pinned native libraries' glibc/libstdc++ ABI.
# Preserve upstream's build tags and its documented Sonic/plugin linker flag.
mkdir -p /out/licenses /out/include
export CGO_LDFLAGS='-L/milvus/lib -Wl,-rpath-link,/milvus/lib'
export GOMEMLIMIT=750MiB
test "$(go env GOVERSION)" = go1.26.10
echo "Building Go planparser and its C++ bridge"

go build -mod=readonly -trimpath -buildvcs=false -buildmode=c-shared \
    -o /out/libmilvus-planparser.so ./internal/parser/planparserv2/cwrapper
# Preserve upstream's shared-object permissions (also lets ELF scanners inspect it).
chmod 0755 /out/libmilvus-planparser.so
mv /out/libmilvus-planparser.h /out/include/
cp internal/parser/planparserv2/cwrapper/milvus_plan_parser.h /out/include/
g++ -std=c++17 -shared -fPIC \
    -o /out/libmilvus-planparser-cpp.so \
    internal/parser/planparserv2/cwrapper/milvus_plan_parser.cpp \
    -I/out/include -L/out -lmilvus-planparser -Wl,-rpath,'$ORIGIN'
echo "Building Milvus main binary"
# Link the main binary against the rebuilt parser as well as same-release C++.
cp /out/libmilvus-planparser.so /out/libmilvus-planparser-cpp.so /milvus/lib/
go build -mod=readonly -trimpath -buildvcs=false \
    -tags dynamic,sonic,with_jemalloc,bytedance_tango \
    -ldflags="-checklinkname=0 -r /milvus/lib -X github.com/milvus-io/milvus/cmd/milvus.BuildTags=dynamic,sonic,with_jemalloc,bytedance_tango -X github.com/milvus-io/milvus/cmd/milvus.GitCommit=328df0cd2f323c2d3c50cef91bfc9f2c412bcb09 -X github.com/milvus-io/milvus/cmd/milvus.GoVersion=go1.26.10 -X github.com/milvus-io/milvus/cmd/milvus.MilvusVersion=v3.0.3" \
    -o /out/milvus ./cmd/main.go

# Exercise the upstream Avro snapshot contract with the replacement decoder.
# These cases use in-memory protobuf/Avro fixtures and do not contact services.
go test -mod=readonly -tags dynamic,sonic,with_jemalloc,bytedance_tango \
    -ldflags=-checklinkname=0 -count=1 \
    -run '^Test(ManifestSchemaByVersion|ParseSnapshotMetadataWithVersionCheck|SegmentManifestRoundTrip|ParseSegmentManifestV2DefaultsCommitTimestamp|OptionalManifestConversions)$' \
    ./internal/snapshotio

cp LICENSE /out/licenses/Milvus-LICENSE
cp "$(go env GOROOT)/LICENSE" /out/licenses/Go-LICENSE
cp go.mod go.sum /out/licenses/
cp pkg/go.mod /out/licenses/pkg.go.mod
cp pkg/go.sum /out/licenses/pkg.go.sum
cp client/go.mod /out/licenses/client.go.mod
cp client/go.sum /out/licenses/client.go.sum
go version -m /out/milvus /out/libmilvus-planparser.so > /out/licenses/build-info.txt
# Copy copyright/notice files for every linked Go module, including replacements.
go list -mod=readonly -tags dynamic,sonic,with_jemalloc,bytedance_tango -deps \
    -f '{{if .Module}}{{if .Module.Replace}}{{.Module.Replace.Path}} {{.Module.Replace.Dir}}{{else}}{{.Module.Path}} {{.Module.Dir}}{{end}}{{end}}' \
    ./cmd/main.go > /tmp/linked-modules
go list -mod=readonly -tags dynamic,sonic,with_jemalloc,bytedance_tango -deps \
    -f '{{if .Module}}{{if .Module.Replace}}{{.Module.Replace.Path}} {{.Module.Replace.Dir}}{{else}}{{.Module.Path}} {{.Module.Dir}}{{end}}{{end}}' \
    ./internal/parser/planparserv2/cwrapper >> /tmp/linked-modules
sort -u /tmp/linked-modules | while read -r module directory; do
    [ -n "$module" ] || continue
    target="/out/licenses/$(printf '%s' "$module" | tr '/' '_')"
    mkdir -p "$target"
    find "$directory" -maxdepth 1 -type f \
        \( -iname 'LICENSE*' -o -iname 'LICENCE*' -o -iname 'NOTICE*' -o -iname 'COPYING*' \) \
        -exec cp {} "$target/" \;
done
