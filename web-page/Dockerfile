# статичный сайт tgops.xyz в nginx, для cloud run (порт 8080)
FROM nginx:alpine

COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY . /usr/share/nginx/html

# удаляем дефолтный конфиг во избежание конфликтов
RUN rm -f /usr/share/nginx/html/Dockerfile /usr/share/nginx/html/nginx.conf

EXPOSE 8080
