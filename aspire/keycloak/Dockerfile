# syntax=docker/dockerfile:1
# Keycloak dev image with the keycloak-kafka event-listener SPI baked in, so KC
# emits user + admin events to Kafka (consumed by Pivox for identity sync).
#
# KC_VERSION is the single source of truth: it drives BOTH the SPI build target
# (-Dkeycloak.version) AND the runtime base image, so they can't drift. It's
# declared global (before any FROM) so the stage-2 FROM can interpolate it, and
# re-declared inside stage 1 so that stage's RUN can see it.
ARG KC_VERSION=26.6.4
ARG KAFKA_VERSION=4.3.1
ARG SPI_REF=master

# --- Stage 1: build the SPI fat jar (KC 26 requires JDK 21) ---
FROM maven:3.9-eclipse-temurin-21 AS spi
ARG KC_VERSION
ARG KAFKA_VERSION
ARG SPI_REF
RUN git clone --depth 1 --branch "${SPI_REF}" https://github.com/dashkan/keycloak-kafka.git /src
WORKDIR /src
RUN mvn -q -B package -Dkeycloak.version="${KC_VERSION}" -Dkafka.version="${KAFKA_VERSION}"

# --- Stage 2: Keycloak with the provider deployed + registered ---
FROM quay.io/keycloak/keycloak:${KC_VERSION}
COPY --from=spi /src/target/keycloak-kafka-*-jar-with-dependencies.jar /opt/keycloak/providers/
# Register the provider into the image. The Aspire dev default (start-dev)
# auto-augments on boot and would pick up the jar anyway, but building here keeps
# it available for `start --optimized` too and surfaces SPI/build errors at
# image-build time rather than at container start. --db=postgres matches the
# runtime KC_DB (set in apphost.mts) so the baked build doesn't warn / mismatch.
RUN /opt/keycloak/bin/kc.sh build --db=postgres
