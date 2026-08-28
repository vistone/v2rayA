#!/usr/bin/env bash

set -ex
CurrentDir="$(pwd)"

if [ -f "$CurrentDir/VERSION" ]; then
  # 版本号遵循官方 v2rayA 惯例：VERSION 文件存 tag 名（vX.Y.Z），
  # 嵌入二进制时去掉前缀 v（与官方 release workflow 的 sed 's/v//g' 一致）。
  version="$(cat "$CurrentDir/VERSION" | sed 's/^v//')"
elif [ -d "$CurrentDir/.git" ]; then
  date=$(git -C "$CurrentDir" log -1 --format="%cd" --date=short | sed s/-//g)
  count=$(git -C "$CurrentDir" rev-list --count HEAD)
  commit=$(git -C "$CurrentDir" rev-parse --short HEAD)
  version="unstable-$date.r${count}.$commit"
else
  version="unstable"
fi
# https://github.com/webpack/webpack/issues/14532#issuecomment-947012063
mkdir -p "$CurrentDir"/bin
# GUI 构建产物统一进 bin/web，同时拷贝进 embed 目录随二进制内嵌（与 CI 一致），
# 使 bin/v2raya 自带 GUI，运行时无需 --webdir；源码区仅保留占位文件。
cd "$CurrentDir"/gui && yarn --ignore-engines && OUTPUT_DIR="$CurrentDir"/bin/web yarn --ignore-engines build
rm -rf "$CurrentDir"/service/server/router/web
mkdir -p "$CurrentDir"/service/server/router/web
cp -r "$CurrentDir"/bin/web/* "$CurrentDir"/service/server/router/web/

# Build v2raya-core (merged xray-core + custom protocols)
cd "$CurrentDir"/core && CGO_ENABLED=0 go build -trimpath -ldflags "-X main.Version=$version -s -w" -o "$CurrentDir"/bin/v2raya_core ./main

cd "$CurrentDir"/service && CGO_ENABLED=0 go build -trimpath -tags "with_gvisor" -ldflags "-X github.com/v2rayA/v2rayA/conf.Version=$version -s -w" -o "$CurrentDir"/bin/v2raya
# 恢复占位 web，避免污染工作区（非 git 仓库或失败时忽略）
git -C "$CurrentDir" checkout -- service/server/router/web 2>/dev/null || true
