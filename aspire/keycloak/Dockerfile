# syntax=docker/dockerfile:1
# Keycloak dev image with the Pivox provider SPIs baked in:
#   - kafka event listener  -> emits user + admin events to Kafka (identity sync)
#   - select-organization   -> post-auth org picker that binds an org into the token
# Both ship in one jar (dashkan/pivox-keycloak-spi, jar-with-dependencies).
#
# KC_VERSION is the single source of truth for Keycloak: it drives BOTH the SPI
# build target (-Dkeycloak.version) AND the runtime base image, so they can't
# drift. Declared global (before any FROM) so the stage-2 FROM can interpolate
# it, and re-declared inside each stage that needs it.
ARG KC_VERSION=26.6.4
ARG KAFKA_VERSION=4.3.1
# PIVOX_KC_SPI_VERSION is the single knob for the SPI: it selects the git tag to
# build (v$VERSION, a reproducible ref — not main/HEAD) AND the jar version
# (-Drevision, since the pom uses ${revision}). Bump it to pull + bake a new SPI
# release; the change busts the clone/build cache so the new tag is actually
# fetched. Must match a pushed `v<version>` tag on dashkan/pivox-keycloak-spi.
ARG PIVOX_KC_SPI_VERSION=1.0.1

# --- Stage 1: build the SPI fat jar (KC 26 requires JDK 21) ---
FROM maven:3.9-eclipse-temurin-21 AS spi
ARG KC_VERSION
ARG KAFKA_VERSION
ARG PIVOX_KC_SPI_VERSION
# GITHUB_PAT: read-only PAT to clone the private dashkan/pivox-keycloak-spi repo.
# Passed as a build arg from apphost.mts, which sources it from .envrc's
# PIVOX_KEYCLOAK_SPI_GITHUB_PAT. Dev-only image — the token is not scrubbed from
# build layers (accepted).
ARG GITHUB_PAT
RUN git clone --depth 1 --branch "v${PIVOX_KC_SPI_VERSION}" \
    "https://x-access-token:${GITHUB_PAT}@github.com/dashkan/pivox-keycloak-spi.git" /src
WORKDIR /src
RUN mvn -q -B package -Drevision="${PIVOX_KC_SPI_VERSION}" \
    -Dkeycloak.version="${KC_VERSION}" -Dkafka.version="${KAFKA_VERSION}"

# --- Stage 2: Keycloak with the providers deployed + registered ---
FROM quay.io/keycloak/keycloak:${KC_VERSION}
COPY --from=spi /src/target/pivox-keycloak-spi-*-jar-with-dependencies.jar /opt/keycloak/providers/
# Register the providers into the image. The Aspire dev default (start-dev)
# auto-augments on boot and would pick up the jar anyway, but building here keeps
# it available for `start --optimized` too and surfaces SPI/build errors at
# image-build time rather than at container start. --db=postgres matches the
# runtime KC_DB (set in apphost.mts) so the baked build doesn't warn / mismatch.
RUN /opt/keycloak/bin/kc.sh build --db=postgres
