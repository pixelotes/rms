# Minimal ffmpeg/ffprobe with only what rms uses (see docs/features/streaming.md):
# remux and transcode of common video files to MP4 (H.264 + AAC), poster
# resizing (JPEG/PNG/WebP) and video thumbnails. Alpine's ffmpeg package pulls
# in ~100 MB of libraries; this build needs only x264, libwebp and zlib.
FROM alpine:3.24 AS ffmpeg

ARG FFMPEG_VERSION=8.1.3
ARG FFMPEG_SHA256=7138d28c96d9d3e3af4ee3d8cad72741f8ffb40da90c1112235dea3ecd3178a3

RUN apk add --no-cache build-base nasm pkgconf x264-dev libwebp-dev zlib-dev curl xz
WORKDIR /src
RUN curl -fsSL -o ffmpeg.tar.xz "https://ffmpeg.org/releases/ffmpeg-${FFMPEG_VERSION}.tar.xz" \
 && echo "${FFMPEG_SHA256}  ffmpeg.tar.xz" | sha256sum -c - \
 && tar xf ffmpeg.tar.xz --strip-components=1
RUN ./configure \
      --prefix=/opt/ffmpeg \
      --disable-everything --disable-autodetect --disable-doc --disable-debug \
      --disable-network --disable-ffplay --disable-avdevice \
      --enable-gpl --enable-libx264 --enable-libwebp --enable-zlib \
      --enable-ffmpeg --enable-ffprobe --enable-swscale --enable-swresample \
      --enable-protocol=file,pipe \
      --enable-demuxer=matroska,mov,avi,mpegts,mpegps,flv,asf,ogg,mp3,aac,ac3,eac3,flac,wav,image2,image_jpeg_pipe,image_png_pipe,image_webp_pipe \
      --enable-muxer=mp4,image2 \
      --enable-decoder=h264,hevc,mpeg4,mpeg2video,mpeg1video,msmpeg4v3,vc1,wmv3,vp8,vp9,mjpeg,png,webp \
      --enable-decoder=aac,aac_latm,ac3,eac3,dca,truehd,mlp,mp3,mp2,flac,opus,vorbis,wmav2,pcm_s16le,pcm_s24le,pcm_s16be \
      --enable-encoder=libx264,aac,mjpeg,png,libwebp \
      --enable-parser=h264,hevc,mpeg4video,mpegvideo,vc1,vp8,vp9,av1,aac,aac_latm,ac3,dca,mlp,mpegaudio,flac,opus,vorbis,mjpeg,png,webp \
      --enable-bsf=aac_adtstoasc,extract_extradata,h264_mp4toannexb,hevc_mp4toannexb,vp9_superframe,av1_frame_merge \
      --enable-filter=scale,format,aformat,aresample,null,anull,copy,acopy \
 && make -j"$(nproc)" && make install \
 && strip /opt/ffmpeg/bin/ffmpeg /opt/ffmpeg/bin/ffprobe

FROM golang:1.27-alpine AS builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o rms ./cmd/rms
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o metacrawler ./cmd/metacrawler
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o subcrawler ./cmd/subcrawler

FROM alpine:3.24

RUN apk add --no-cache tzdata x264-libs libwebp libwebpmux zlib

WORKDIR /app
COPY --from=ffmpeg /opt/ffmpeg/bin/ffmpeg /opt/ffmpeg/bin/ffprobe /usr/local/bin/
COPY --from=builder /build/rms .
COPY --from=builder /build/metacrawler .
COPY --from=builder /build/subcrawler .
COPY --from=builder /build/web ./web

EXPOSE 8082

ENTRYPOINT ["./rms", "-config", "/app/config/config.yml"]
