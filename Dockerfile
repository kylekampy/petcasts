FROM python:3.13-slim

WORKDIR /app

# Install system deps for Pillow
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        libjpeg62-turbo-dev \
        libpng-dev \
        libfreetype6-dev \
        fonts-dejavu-core \
    && rm -rf /var/lib/apt/lists/*

# Copy source and install
COPY pyproject.toml .
COPY src/ src/
COPY config.yaml .
COPY pets/ pets/

# Create output dirs
RUN mkdir -p output/debug output/archive

RUN pip install --no-cache-dir .

ENV PYTHONUNBUFFERED=1
EXPOSE 7777
CMD ["python", "-m", "petcast", "serve"]
