FROM node:26-alpine AS build
ARG VITE_WORKOS_CLIENT_ID
ENV VITE_WORKOS_CLIENT_ID=$VITE_WORKOS_CLIENT_ID
RUN npm install --global pnpm@11.24.0
WORKDIR /src
COPY package.json pnpm-workspace.yaml pnpm-lock.yaml ./
COPY apps/web/package.json apps/web/package.json
RUN pnpm install --frozen-lockfile
COPY apps/web apps/web
RUN pnpm --dir apps/web build

FROM nginx:1.29-alpine
COPY infra/docker/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /src/apps/web/dist /usr/share/nginx/html
EXPOSE 80
