# Development image for the dashboard component (src/dashboard).
#
# The image ships only the Node toolchain; the source tree is bind-mounted at
# runtime and dependencies are installed by the entrypoint, so a dependency
# change is picked up without rebuilding the image.

FROM node:24-bookworm-slim

ARG UID=1001
ARG GID=1001

ENV npm_config_cache=/npm-cache

RUN groupadd -g "${GID}" dev \
 && useradd -u "${UID}" -g "${GID}" -m -s /bin/bash dev \
 && mkdir -p "${npm_config_cache}" \
 && chown -R "${UID}:${GID}" "${npm_config_cache}"

USER dev
WORKDIR /app

ENTRYPOINT ["/etc/swallow/dashboard-entrypoint.sh"]
