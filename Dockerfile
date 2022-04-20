FROM archlinux:base-devel

RUN pacman -Syu --noconfirm go meson libvips

COPY ./core /app/build/core
COPY ./server /app/build/server

WORKDIR /app/build/core
RUN meson build && cd build && meson compile

WORKDIR /app/build/server
RUN go build -o gifspin-server main.go


FROM archlinux:base

RUN pacman -Syu --noconfirm libvips

COPY --from=0 /app/build/core/build/gifspin-core /app/bin/gifspin-core
COPY --from=0 /app/build/server/gifspin-server /app/bin/gifspin-server

RUN mkdir -p /app/public /data

CMD ["/app/bin/gifspin-server"]
