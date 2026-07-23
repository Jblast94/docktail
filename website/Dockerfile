# Build with the repository root as context:
# docker build -f website/Dockerfile .
FROM golang:1.25-alpine AS docs

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY docs ./docs
COPY tools/docsgen ./tools/docsgen
COPY website ./website

RUN go run ./tools/docsgen -source docs -website website

FROM nginx:alpine

COPY --from=docs /src/website/index.html /usr/share/nginx/html/index.html
COPY --from=docs /src/website/cloud /usr/share/nginx/html/cloud
COPY --from=docs /src/website/docs /usr/share/nginx/html/docs
COPY --from=docs /src/website/assets /usr/share/nginx/html/assets
COPY --from=docs /src/website/robots.txt /usr/share/nginx/html/robots.txt
COPY --from=docs /src/website/sitemap.xml /usr/share/nginx/html/sitemap.xml
COPY --from=docs /src/website/llms.txt /usr/share/nginx/html/llms.txt
COPY --from=docs /src/website/llms-full.txt /usr/share/nginx/html/llms-full.txt
COPY --from=docs /src/website/docs.md /usr/share/nginx/html/docs.md
