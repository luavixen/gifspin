FROM archlinux:base-devel

RUN pacman -Syu --noconfirm nodejs-lts-gallium npm go meson libvips

COPY ./core /app/build/core
COPY ./server /app/build/server
COPY ./website /app/build/website

WORKDIR /app/build/core
RUN meson build && cd build && meson compile

WORKDIR /app/build/server
RUN go build -o gifspin-server main.go

WORKDIR /app/build/website
RUN npm ci && npm run build

FROM archlinux:base

RUN pacman -Syu --noconfirm libvips

COPY --from=0 /app/build/core/build/gifspin-core /app/bin/gifspin-core
COPY --from=0 /app/build/server/gifspin-server /app/bin/gifspin-server
COPY --from=0 /app/build/website/dist /app/public

RUN mkdir -p /data

CMD ["/app/bin/gifspin-server"]
