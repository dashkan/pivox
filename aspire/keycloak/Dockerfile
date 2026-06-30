# syntax=docker/dockerfile:1
# Keycloak dev image with the keycloak-kafka event-listener SPI baked in, so KC
# emits user + admin events to Kafka (consumed by Pivox for identity sync).
#
# KC_VERSION is the single source of truth for Keycloak: it drives BOTH the SPI
# build target (-Dkeycloak.version) AND the runtime base image, so they can't
# drift. Declared global (before any FROM) so the stage-2 FROM can interpolate
# it, and re-declared inside each stage that needs it.
ARG KC_VERSION=26.6.4
ARG KAFKA_VERSION=4.3.1
# KEYCLOAK_KAFKA_VERSION is the single knob for the SPI: it selects the git tag
# to build (v$VERSION, a reproducible ref — not master/HEAD) AND the jar version
# (-Drevision, since the fork's pom uses ${revision}). Bump it to pull + bake a
# new SPI release; the change busts the clone/build cache so the new tag is
# actually fetched. Must match a pushed `v<version>` tag on dashkan/keycloak-kafka.
ARG KEYCLOAK_KAFKA_VERSION=2.0.0

# --- Stage 1: build the SPI fat jar (KC 26 requires JDK 21) ---
FROM maven:3.9-eclipse-temurin-21 AS spi
ARG KC_VERSION
ARG KAFKA_VERSION
ARG KEYCLOAK_KAFKA_VERSION
RUN git clone --depth 1 --branch "v${KEYCLOAK_KAFKA_VERSION}" https://github.com/dashkan/keycloak-kafka.git /src
WORKDIR /src
RUN mvn -q -B package -Drevision="${KEYCLOAK_KAFKA_VERSION}" \
    -Dkeycloak.version="${KC_VERSION}" -Dkafka.version="${KAFKA_VERSION}"

# --- Stage 2: Keycloak with the provider deployed + registered ---
FROM quay.io/keycloak/keycloak:${KC_VERSION}
COPY --from=spi /src/target/keycloak-kafka-*-jar-with-dependencies.jar /opt/keycloak/providers/
# Register the provider into the image. The Aspire dev default (start-dev)
# auto-augments on boot and would pick up the jar anyway, but building here keeps
# it available for `start --optimized` too and surfaces SPI/build errors at
# image-build time rather than at container start. --db=postgres matches the
# runtime KC_DB (set in apphost.mts) so the baked build doesn't warn / mismatch.
RUN /opt/keycloak/bin/kc.sh build --db=postgres
