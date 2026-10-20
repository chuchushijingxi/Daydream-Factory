#!/bin/sh
set -eu

export DEBIAN_FRONTEND=noninteractive
FFMPEG_VERSION=9.0.2
FFMPEG_SIGNER=FCF986EA15E6E293A5644F10B4322F04D67658D8
X264_SOURCE_VERSION=2:0.164.3108+git31e19f9-2
WEBP_VERSION=1.6.0
ZLIB_SOURCE_VERSION=1:1.3.dfsg+really1.3.1-1
PNG_SOURCE_VERSION=1.6.48-1+deb13u6
JPEG_SOURCE_VERSION=1:2.1.5-4

# Binary NMU suffixes vary by architecture; the source versions stay fixed.
sed -i 's/Types: deb$/Types: deb deb-src/' /etc/apt/sources.list.d/debian.sources
apt-get update
apt-get install -y --no-install-recommends \
    build-essential ca-certificates curl gnupg nasm pkg-config xz-utils \
    "libx264-dev=${X264_SOURCE_VERSION}*" \
    "libpng-dev=${PNG_SOURCE_VERSION}" \
    "libjpeg62-turbo-dev=${JPEG_SOURCE_VERSION}" \
    "zlib1g-dev=${ZLIB_SOURCE_VERSION}*"

mkdir -p /build /opt/media/bin /opt/media/licenses /opt/media/source
cd /opt/media/source
curl --fail --location --retry 3 --proto '=https' \
    "https://ffmpeg.org/releases/ffmpeg-${FFMPEG_VERSION}.tar.xz" \
    -o "ffmpeg-${FFMPEG_VERSION}.tar.xz"
curl --fail --location --retry 3 --proto '=https' \
    "https://storage.googleapis.com/downloads.webmproject.org/releases/webp/libwebp-${WEBP_VERSION}.tar.gz" \
    -o "libwebp-${WEBP_VERSION}.tar.gz"
cp /setup/media/ffmpeg-release.asc "ffmpeg-${FFMPEG_VERSION}.tar.xz.asc"

# Verify the official release signature as well as the fixed SHA-256.
export GNUPGHOME=/build/gnupg
mkdir -m 0700 "$GNUPGHOME"
gpg --batch --import /setup/media/ffmpeg-devel.asc
gpg --batch --status-fd 1 --verify "ffmpeg-${FFMPEG_VERSION}.tar.xz.asc" \
    "ffmpeg-${FFMPEG_VERSION}.tar.xz" > /build/signature-status
grep -F "[GNUPG:] VALIDSIG ${FFMPEG_SIGNER} " /build/signature-status

# Preserve exact corresponding sources alongside the independently licensed
# tools. Downloading from the signed Debian index verifies source provenance.
apt-get source --download-only "x264=${X264_SOURCE_VERSION}" \
    "zlib=${ZLIB_SOURCE_VERSION}" "libpng1.6=${PNG_SOURCE_VERSION}" \
    "libjpeg-turbo=${JPEG_SOURCE_VERSION}"
sha256sum -c /setup/media/source-sha256.txt
dpkg-query -W -f='${Package}\t${Version}\t${source:Package}\t${source:Version}\n' \
    libx264-dev libpng-dev libjpeg62-turbo-dev zlib1g-dev \
    > /opt/media/source/linked-packages.tsv
cp /setup/media/source-sha256.txt /opt/media/source/SHA256SUMS
cp /setup/media/build.sh /opt/media/source/build.sh
mkdir /opt/media/source/build-inputs
cp /setup/media/* /opt/media/source/build-inputs/
cp /setup/media/NOTICE.txt /opt/media/NOTICE.txt
cp /usr/share/doc/libx264-dev/copyright /opt/media/licenses/x264-copyright
cp /usr/share/doc/libpng-dev/copyright /opt/media/licenses/libpng-copyright
cp /usr/share/doc/libjpeg62-turbo-dev/copyright /opt/media/licenses/libjpeg-turbo-copyright
cp /usr/share/doc/zlib1g-dev/copyright /opt/media/licenses/zlib-copyright

# Prefer only these static archives. Leave glibc dynamic so the runtime's normal
# Debian security updates apply to libc rather than freezing a second copy.
mkdir /build/static
library_dir="/usr/lib/$(gcc -dumpmachine)"
for library in x264 png16 jpeg z; do
    cp "$library_dir/lib${library}.a" /build/static/
done

# cwebp's only file consumers here are JPEG/PNG, and dwebp writes PNG. Build the
# current official WebP release without the distribution's TIFF/GL/SDL tool tree.
tar -xzf "libwebp-${WEBP_VERSION}.tar.gz" -C /build
cd "/build/libwebp-${WEBP_VERSION}"
./configure --prefix=/build/webp --disable-shared --enable-static \
    --disable-gl --disable-sdl --disable-gif --disable-tiff --disable-wic \
    --disable-libwebpmux --enable-libwebpdemux --enable-libwebpdecoder \
    LDFLAGS=-L/build/static LIBS='-lz -lm'
make -j2
make install
cp /build/webp/bin/cwebp /build/webp/bin/dwebp /opt/media/bin/
cp /build/webp/lib/libwebp.a /build/webp/lib/libsharpyuv.a /build/static/
cp COPYING PATENTS AUTHORS /opt/media/licenses/
cp src/webp/config.h /opt/media/source/webp-config.h
export PKG_CONFIG_PATH=/build/webp/lib/pkgconfig

tar -xJf "/opt/media/source/ffmpeg-${FFMPEG_VERSION}.tar.xz" -C /build
cd "/build/ffmpeg-${FFMPEG_VERSION}"
cp COPYING.* LICENSE.md /opt/media/licenses/

# Only local file/pipe input is needed. Keep the application's advertised image
# formats, MP4 video decoding, orientation/resize filters and local mock encoder.
# Do not autodetect network, XML, GPU, desktop or unrelated codec dependencies.
./configure \
    --prefix=/opt/media \
    --disable-debug --disable-doc --disable-shared --enable-static \
    --disable-autodetect --disable-network --disable-everything \
    --enable-gpl --enable-ffmpeg --enable-ffprobe \
    --enable-avcodec --enable-avformat --enable-avfilter --enable-avdevice \
    --enable-swscale --disable-swresample \
    --enable-libx264 --enable-libwebp --enable-zlib \
    --pkg-config-flags=--static --extra-ldflags=-L/build/static \
    --enable-protocol=file,pipe \
    --enable-demuxer=mov,matroska,image2,image2pipe,image_jpeg_pipe,image_png_pipe,image_webp_pipe,image_bmp_pipe,image_gif_pipe,gif,ico \
    --enable-decoder=h264,hevc,mpeg4,vp8,vp9,mjpeg,png,gif,webp,bmp,wrapped_avframe \
    --enable-parser=h264,hevc,mpeg4video,vp8,vp9,mjpeg,png,gif,webp,bmp \
    --enable-encoder=libx264,libwebp,png,mjpeg,bmp \
    --enable-muxer=mp4,mov,image2,image2pipe,webp \
    --enable-indev=lavfi \
    --enable-filter=color,concat,fps,scale,setsar,format,transpose,hflip,vflip,rotate,trim,setpts,null

# Docker Desktop also hosts the developer's live services; cap compile memory.
make -j2
make install
cp config.h config_components.h ffbuild/config.mak /opt/media/source/
rm -rf /opt/media/include /opt/media/lib /opt/media/share
strip /opt/media/bin/ffmpeg /opt/media/bin/ffprobe /opt/media/bin/cwebp /opt/media/bin/dwebp

# Fail a build if an optional library silently became dynamically required.
for executable in /opt/media/bin/*; do
    readelf -d "$executable" | sed -n 's/.*Shared library: \[\(.*\)\].*/\1/p' \
        > /build/needed-libraries
    if grep -Ev '^(libc\.so\.6|libm\.so\.6|ld-linux.*\.so\.[0-9]+)$' /build/needed-libraries; then
        echo "unexpected non-glibc dynamic dependency: $executable" >&2
        exit 1
    fi
done
/opt/media/bin/ffmpeg -version
/opt/media/bin/ffprobe -version
/opt/media/bin/cwebp -version
/opt/media/bin/dwebp -version
