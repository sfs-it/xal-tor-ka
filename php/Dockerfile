# xtk-php — immagine STANDARD php-fpm per l'hosting Xal-Tor-Ka (stack php-fpm / laravel).
# Contiene: il set COMUNE di estensioni già cotto (copre il ~95%: Laravel/CMS) + lo strumento
# install-php-extensions (RESTA a bordo: serve a caldo) + composer + l'entrypoint che
# materializza i moduli VARIABILI scelti dall'admin (env XTK_PHP_EXT), sull'immagine standard,
# SENZA rebuild per-vhost. Buildata sul box come xtk-php:<ver> (come nginx/Dockerfile per il WAF).
ARG PHP_VERSION=8.3
FROM php:${PHP_VERSION}-fpm-alpine

# lo strumento estensioni resta nell'immagine (materializzazione a caldo dei moduli à-la-carte)
COPY --from=mlocati/php-extension-installer:2 /usr/bin/install-php-extensions /usr/local/bin/

# set COMUNE pre-cotto (baseline Laravel/CMS)
RUN install-php-extensions pdo_pgsql pdo_mysql bcmath gd zip intl opcache pcntl

# composer a bordo (composer install sul box; oppure l'owner spedisce vendor/ via SCP)
COPY --from=composer:2 /usr/bin/composer /usr/local/bin/composer

# entrypoint materializzatore dei moduli variabili
COPY xtk-entrypoint.sh /usr/local/bin/xtk-entrypoint.sh
RUN chmod +x /usr/local/bin/xtk-entrypoint.sh

ENTRYPOINT ["/usr/local/bin/xtk-entrypoint.sh"]
CMD ["php-fpm"]
