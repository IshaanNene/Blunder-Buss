FROM nginx:alpine
RUN apk add --no-cache netcat-openbsd
COPY web /usr/share/nginx/html
COPY docker/web/nginx.conf /etc/nginx/conf.d/default.conf
RUN chmod -R 755 /usr/share/nginx/html
EXPOSE 80