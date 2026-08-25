#!/usr/bin/env bash

set -ex
CurrentDir="$(pwd)"

if [ -d "$CurrentDir/.git" ]; then
  date=$(git -C "$CurrentDir" log -1 --format="%cd" --date=short | sed s/-//g)
  count=$(git -C "$CurrentDir" rev-list --count HEAD)
  commit=$(git -C "$CurrentDir" rev-parse --short HEAD)
  version="unstable-$date.r${count}.$commit"
else
  version="unstable"
fi
# https://github.com/webpack/webpack/issues/14532#issuecomment-947012063
mkdir -p "$CurrentDir"/bin
# GUI 构建产物统一进 bin/web，运行时用 --webdir 指向（源码区 service/server/router/web 仅保留 embed 占位）
cd "$CurrentDir"/gui && yarn --ignore-engines && OUTPUT_DIR="$CurrentDir"/bin/web yarn --ignore-engines build

# Build v2raya-core (merged xray-core + custom protocols)
cd "$CurrentDir"/core && CGO_ENABLED=0 go build -trimpath -ldflags "-X main.Version=$version -s -w" -o "$CurrentDir"/bin/v2raya_core ./main

cd "$CurrentDir"/service && CGO_ENABLED=0 go build -trimpath -tags "with_gvisor" -ldflags "-X github.com/v2rayA/v2rayA/conf.Version=$version -s -w" -o "$CurrentDir"/bin/v2raya
