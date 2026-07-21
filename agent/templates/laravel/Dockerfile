# Laravel-ready PHP-FPM image for Xal-Tor-Ka hosting (stack "laravel").
# Same runtime model as the php-fpm template (runs as an arbitrary site uid via the
# compose `user:` override, fpm on :9000) — we only ADD the compiled extensions Laravel
# needs (esp. pdo_pgsql: the stock php:*-fpm-alpine lacks it → "could not find driver")
# plus composer. Built once on the box as xtk-php-laravel:<ver> (like nginx/Dockerfile).
ARG PHP_VERSION=8.3
FROM php:${PHP_VERSION}-fpm-alpine

# mlocati/install-php-extensions resolves the alpine build deps for each extension.
COPY --from=mlocati/php-extension-installer:2 /usr/bin/install-php-extensions /usr/local/bin/
RUN install-php-extensions \
      pdo_pgsql pdo_mysql bcmath gd zip intl opcache pcntl \
 && rm -f /usr/local/bin/install-php-extensions

# composer on the box (optional convenience; the owner may also ship vendor/ via SCP).
COPY --from=composer:2 /usr/bin/composer /usr/local/bin/composer
