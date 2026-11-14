FROM node:26-alpine AS dependencies
WORKDIR /app
RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile

FROM node:26-alpine AS build
WORKDIR /app
RUN corepack enable
COPY --from=dependencies /app/node_modules ./node_modules
COPY web/ ./
RUN pnpm build

FROM node:26-alpine AS runtime
ENV NODE_ENV=production
ENV PORT=3000
ENV HOSTNAME=0.0.0.0
WORKDIR /app
RUN addgroup -S relay && adduser -S -G relay relay
COPY --from=build --chown=relay:relay /app/.next/standalone ./
COPY --from=build --chown=relay:relay /app/.next/static ./.next/static
COPY --from=build --chown=relay:relay /app/public ./public
USER relay
EXPOSE 3000
CMD ["node", "server.js"]
