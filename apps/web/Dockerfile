# Multi-stage build: compile the Vite app, then serve the static bundle with
# nginx. The API base URL is a build-time value (Vite inlines import.meta.env
# at build time), so it must be supplied as a build arg, not a runtime env var.
ARG VITE_API_BASE_URL=http://localhost:8080

FROM node:22-alpine AS build
WORKDIR /src
COPY package.json package-lock.json* ./
RUN npm ci
COPY . .
ARG VITE_API_BASE_URL
ENV VITE_API_BASE_URL=${VITE_API_BASE_URL}
RUN npm run build

FROM nginx:1.27-alpine
COPY --from=build /src/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
HEALTHCHECK --interval=10s --timeout=5s --retries=5 CMD wget -q -O- http://localhost/ || exit 1
