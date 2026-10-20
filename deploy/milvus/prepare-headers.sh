#!/bin/sh
set -eu
# Only C ABI headers are needed by cgo; no C++ dependency build is required.
mkdir -p /tmp/common /tmp/storage /milvus/include /milvus/lib/pkgconfig
tar -xzf /tmp/common.tar.gz --strip-components=1 -C /tmp/common
tar -xzf /tmp/storage.tar.gz --strip-components=1 -C /tmp/storage
cp -R /tmp/common/include/. /milvus/include/
cp -R /tmp/storage/cpp/include/. /milvus/include/
cp -R /src/internal/core/src/. /milvus/include/
# Upstream .pc templates contain only the library name and include/lib paths.
for spec in 'milvus_core:milvus_core:3.0.3' 'milvus-storage:milvus-storage:3.0.3' 'rdkafka:rdkafka:2.6.1' 'rocksdb:rocksdb:6.29.5' 'jemalloc:jemalloc:5.2.1'; do
    name=${spec%%:*}; rest=${spec#*:}; library=${rest%%:*}; version=${rest#*:}
    cat > "/milvus/lib/pkgconfig/$name.pc" <<PC
Name: $name
Description: Same-release Milvus C ABI
Version: $version
Libs: -L/milvus/lib -l$library
Cflags: -I/milvus/include
PC
done
