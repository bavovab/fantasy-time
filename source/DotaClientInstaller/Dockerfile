FROM steamcmd/steamcmd:debian-12@sha256:be620712ead372869b8ed57d710199b6fe916c4ef2cb31b59e4ef6720641436b

USER root

RUN apt-get update \
    && apt-get install -y --no-install-recommends python3 \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 1000 steam \
    && useradd --uid 1000 --gid 1000 --create-home --shell /usr/sbin/nologin steam

COPY installer.py /opt/dota-client-installer/installer.py
RUN chmod 0555 /opt/dota-client-installer/installer.py

ENV HOME=/home/steam
ENV DOTA_INSTALL_DIR=/dota
ENV DOTA_STATUS_DIR=/status
ENV DOTA_EXPECTED_INSTALL_BYTES=90000000000

USER 1000:1000

ENTRYPOINT ["python3", "/opt/dota-client-installer/installer.py"]
