#!/usr/bin/env bash
#
# v2rayA 一键安装脚本（vistone fork，含节点池等自定义功能）
#
# 用法：
#   curl -Ls https://raw.githubusercontent.com/vistone/v2rayA/main/install.sh | sudo bash
#   sudo bash install.sh
#   V2RAYA_VERSION=2.4.16 sudo bash install.sh     # 安装指定版本
#   sudo bash install.sh --uninstall               # 卸载（保留 /etc/v2raya 配置）
#
# 环境变量：
#   V2RAYA_REPO      发布仓库，默认 vistone/v2rayA
#   V2RAYA_VERSION   指定版本号（如 2.4.16），缺省自动获取最新 Release
#   V2RAYA_BASE_URL  下载源前缀（国内可配 GitHub 加速镜像），默认官方地址
#   V2RAYA_NO_START  置 1 时只安装不启动服务
#
# 行为：自动获取版本号/架构/下载地址/校验和，并预装 geoip/geosite 数据文件，
# 无需人工干预；重复执行即为升级，保留 /etc/v2raya 配置与节点数据。
#
set -euo pipefail

# ---------- 基本配置 ----------
REPO="${V2RAYA_REPO:-vistone/v2rayA}"
BASE_URL="${V2RAYA_BASE_URL:-https://github.com/${REPO}}"
API_BASE="https://api.github.com/repos/${REPO}"
LIB_DIR="/usr/local/lib/v2raya"
BIN_DIR="/usr/local/bin"
ASSET_DIR="/usr/local/share/v2raya"
CONF_DIR="/etc/v2raya"
LOG_DIR="/var/log/v2raya"
UNIT_FILE="/etc/systemd/system/v2raya.service"
DEFAULT_FILE="/etc/default/v2raya"
TMP_DIR=""

msg()  { printf '\033[1;32m[install]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[警告]\033[0m %s\n' "$*"; }
die()  { printf '\033[1;31m[错误]\033[0m %s\n' "$*" >&2; exit 1; }

# ---------- 权限检查 ----------
check_root() {
  if [ "$(id -u)" -ne 0 ]; then
    if [ -f "$0" ] && command -v sudo >/dev/null 2>&1; then
      warn "需要 root 权限，自动通过 sudo 重新执行..."
      exec sudo -E bash "$0" "$@"
    fi
    die "请用 root 或 sudo 运行本脚本（例如: curl -Ls <url> | sudo bash）"
  fi
}

# ---------- 依赖检查 ----------
check_deps() {
  for c in curl sha256sum systemctl sed grep awk; do
    command -v "$c" >/dev/null 2>&1 || die "缺少依赖命令: $c"
  done
}

# ---------- 架构检测 → Release 友好文件名 ----------
detect_arch() {
  local m
  m="$(uname -m)"
  case "$m" in
    x86_64|amd64)              ARCH="linux_x64" ;;
    aarch64|arm64)             ARCH="linux_arm64" ;;
    i386|i686|x86)             ARCH="linux_x86" ;;
    armv7l|armhf)              ARCH="linux_armv7" ;;
    armv6l)                    ARCH="linux_armv6" ;;
    armv5tel)                  ARCH="linux_armv5" ;;
    riscv64)                   ARCH="linux_riscv64" ;;
    loongarch64|loong64)       ARCH="linux_loongarch64" ;;
    mips)                      ARCH="linux_mips32" ;;
    mipsel)                    ARCH="linux_mips32le" ;;
    mips64)                    ARCH="linux_mips64" ;;
    mips64el)                  ARCH="linux_mips64le" ;;
    *) die "不支持的架构: $m" ;;
  esac
  msg "检测到架构: $m → $ARCH"
}

# ---------- 版本号解析 ----------
resolve_version() {
  if [ -n "${V2RAYA_VERSION:-}" ]; then
    VERSION="$(printf '%s' "$V2RAYA_VERSION" | sed 's/^v//')"
    msg "使用指定版本: $VERSION"
    return
  fi
  msg "正在获取最新 Release 版本号..."
  # 1) GitHub API（不可达时静默失败，走下方 HTML 兑底）
  VERSION="$(curl -fsSL --connect-timeout 10 -H "Accept: application/vnd.github+json" \
    "${API_BASE}/releases/latest" 2>/dev/null \
    | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' \
    | head -n1 | sed 's/^v//' || true)"
  # 2) 页面跳转兑底
  if [ -z "$VERSION" ]; then
    VERSION="$(curl -fsSI --connect-timeout 10 -o /dev/null -w '%{redirect_url}' \
      "https://github.com/${REPO}/releases/latest" 2>/dev/null \
      | sed -n 's#.*/releases/tag/v\([0-9][0-9.]*\).*#\1#p' || true)"
  fi
  if [ -z "$VERSION" ]; then
    die "未找到 vistone/v2rayA 的 Release。
  首次使用请先发布一个 Release（版本号来自仓库根 VERSION 文件）：
    1. git tag v\$(cat VERSION | sed 's/^v//')
    2. git push origin v\$(cat VERSION | sed 's/^v//')
    3. 在 GitHub Actions 手动运行 release_main.yml（workflow_dispatch，tag 填 vX.Y.Z）
  完成后重新运行本脚本即可；也可用 V2RAYA_VERSION=x.y.z 指定已发布的版本。"
  fi
  msg "最新版本: $VERSION"
}

# ---------- 阶段一：下载全部组件并校验（任何失败即中止，不做任何安装） ----------
download_one() {
  local url="$1" out="$2" sum_url="$3" expected sha
  msg "下载 ${out} ..."
  if ! curl -fL --connect-timeout 15 --retry 3 --retry-delay 2 -o "${TMP_DIR}/${out}" "$url"; then
    die "下载失败: $url"
  fi
  if [ -n "$sum_url" ]; then
    if curl -fsL --connect-timeout 15 --retry 3 -o "${TMP_DIR}/${out}.sha256" "$sum_url"; then
      expected="$(awk '{print $1}' "${TMP_DIR}/${out}.sha256")"
      sha="$(sha256sum "${TMP_DIR}/${out}" | awk '{print $1}')"
      if [ "$sha" != "$expected" ]; then
        die "sha256 校验失败: ${out}\n  期望: $expected\n  实际: $sha"
      fi
      msg "sha256 校验通过: ${out}"
    else
      warn "未获取到校验文件 ${out}.sha256，跳过 sha256 校验"
    fi
  fi
}

download_all() {
  msg "阶段 1/2：下载全部组件（二进制 + 数据文件）并校验..."
  # v2ray-core 数据文件
  download_one \
    "https://github.com/v2fly/geoip/releases/latest/download/geoip.dat" \
    "geoip.dat" \
    "https://github.com/v2fly/geoip/releases/latest/download/geoip.dat.sha256sum"
  download_one \
    "https://github.com/v2fly/domain-list-community/releases/latest/download/dlc.dat" \
    "geosite.dat" \
    "https://github.com/v2fly/domain-list-community/releases/latest/download/dlc.dat.sha256sum"
  # GFWList（分流/PAC 模式需要；Loyalsoldier 不发布独立校验文件）
  download_one \
    "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat" \
    "LoyalsoldierSite.dat" \
    ""
  # v2rayA 二进制
  download_one \
    "${BASE_URL}/releases/download/v${VERSION}/v2raya_${ARCH}_${VERSION}" \
    "v2raya_${ARCH}_${VERSION}" \
    "${BASE_URL}/releases/download/v${VERSION}/v2raya_${ARCH}_${VERSION}.sha256.txt"
  download_one \
    "${BASE_URL}/releases/download/v${VERSION}/v2raya_core_${ARCH}_${VERSION}" \
    "v2raya_core_${ARCH}_${VERSION}" \
    "${BASE_URL}/releases/download/v${VERSION}/v2raya_core_${ARCH}_${VERSION}.sha256.txt"
  msg "全部组件下载并校验完成，开始安装"
}

# ---------- 阶段二：安装（仅在上一步全部下载成功后才执行） ----------
install_all() {
  msg "阶段 2/2：安装到系统..."
  install -d -m 0755 "$LIB_DIR"
  install -m 0755 "${TMP_DIR}/v2raya_${ARCH}_${VERSION}" "${LIB_DIR}/v2raya"
  install -m 0755 "${TMP_DIR}/v2raya_core_${ARCH}_${VERSION}" "${LIB_DIR}/v2raya_core"
  ln -sf "${LIB_DIR}/v2raya" "${BIN_DIR}/v2raya"
  ln -sf "${LIB_DIR}/v2raya_core" "${BIN_DIR}/v2raya_core"
  msg "创建运行目录: 配置 ${CONF_DIR} / 日志 ${LOG_DIR} / 资产 ${ASSET_DIR}"
  install -d -m 0755 "$CONF_DIR" "$ASSET_DIR"
  install -d -m 0750 "$LOG_DIR"
  # 预装数据文件，避免首次启动时 GUI 提示 "Downloading missing geoip.dat and geosite.dat"
  install -m 0644 "${TMP_DIR}/geoip.dat" "${ASSET_DIR}/geoip.dat"
  install -m 0644 "${TMP_DIR}/geosite.dat" "${ASSET_DIR}/geosite.dat"
  install -m 0644 "${TMP_DIR}/LoyalsoldierSite.dat" "${ASSET_DIR}/LoyalsoldierSite.dat"
  msg "数据文件已安装到 ${ASSET_DIR}（geoip.dat / geosite.dat / LoyalsoldierSite.dat）"
}

write_default_env() {
  msg "写入 ${DEFAULT_FILE} ..."
  cat > "$DEFAULT_FILE" <<EOF
# v2rayA 默认环境（由 install.sh 生成，可自行修改）
V2RAYA_LOG_FILE=${LOG_DIR}/v2raya.log
XRAY_LOCATION_ASSET=${ASSET_DIR}
V2RAY_LOCATION_ASSET=${ASSET_DIR}
# 监听地址，默认 0.0.0.0:2017
# V2RAYA_ADDRESS=0.0.0.0:2017
EOF
}

write_systemd_unit() {
  msg "写入 systemd 服务 ${UNIT_FILE} ..."
  cat > "$UNIT_FILE" <<EOF
[Unit]
Description=v2rayA Service (vistone fork)
Documentation=https://github.com/${REPO}
After=network.target network-online.target nss-lookup.target iptables.service ip6tables.service nftables.service
Wants=network.target

[Service]
Type=simple
User=root
LimitNPROC=500
LimitNOFILE=1000000
ExecStart=${BIN_DIR}/v2raya --log-disable-timestamp
EnvironmentFile=-${DEFAULT_FILE}
Restart=on-failure

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
}

stop_old_instance() {
  # 停止并禁用可能存在的旧服务（apt 版或本脚本安装版），避免端口冲突
  if systemctl list-unit-files 2>/dev/null | grep -q '^v2raya\.service'; then
    systemctl stop v2raya 2>/dev/null || true
    systemctl disable v2raya 2>/dev/null || true
  fi
  # 停掉手动拉起、未被 systemd 托管的进程
  if pgrep -x v2raya >/dev/null 2>&1; then
    warn "检测到未托管的 v2raya 进程，正在停止..."
    pkill -x v2raya 2>/dev/null || true
    sleep 1
  fi
}

start_service() {
  if [ "${V2RAYA_NO_START:-}" = "1" ]; then
    msg "V2RAYA_NO_START=1，跳过启动；可稍后执行: systemctl enable --now v2raya"
    return
  fi
  msg "启动 v2raya 服务..."
  systemctl enable v2raya >/dev/null 2>&1 || true
  systemctl start v2raya
  sleep 2
  if [ "$(systemctl is-active v2raya)" != "active" ]; then
    warn "服务未进入 active 状态，请查看日志: journalctl -u v2raya -n 50"
  else
    msg "v2raya 服务已启动（active）"
  fi
}

print_summary() {
  local ip=""
  ip="$(hostname -I 2>/dev/null | awk '{print $1}')"
  msg "安装完成！"
  echo
  printf '\033[1;36m==================================================\033[0m\n'
  printf '\033[1;36m  v2rayA %s (%s) 安装成功\033[0m\n' "$VERSION" "$ARCH"
  if [ "${V2RAYA_NO_START:-}" != "1" ]; then
    printf '  访问地址: http://%s:2017\n' "${ip:-<本机IP>}"
    printf '  状态查看: systemctl status v2raya\n'
    printf '  日志查看: journalctl -u v2raya -f\n'
  fi
  printf '  配置目录: %s   （升级/重装不丢失）\n' "$CONF_DIR"
  printf '  二进制:   %s/v2raya, %s/v2raya_core\n' "$LIB_DIR" "$LIB_DIR"
  printf '\033[1;36m==================================================\033[0m\n'
}

uninstall() {
  msg "卸载 v2rayA ..."
  systemctl stop v2raya 2>/dev/null || true
  systemctl disable v2raya 2>/dev/null || true
  rm -f "$UNIT_FILE"
  systemctl daemon-reload
  rm -rf "$LIB_DIR"
  rm -f "${BIN_DIR}/v2raya" "${BIN_DIR}/v2raya_core"
  # 保留配置与日志，避免误删用户数据
  warn "已保留 ${CONF_DIR}（配置）与 ${LOG_DIR}（日志）；如需彻底删除请手动执行: rm -rf $CONF_DIR $LOG_DIR $ASSET_DIR $DEFAULT_FILE"
  msg "卸载完成"
}

main() {
  if [ "${1:-}" = "--uninstall" ]; then
    uninstall
    return
  fi
  check_root "$@"
  check_deps
  detect_arch
  resolve_version
  TMP_DIR="$(mktemp -d)"
  trap 'rm -rf "$TMP_DIR"' EXIT
  download_all
  stop_old_instance
  install_all
  write_default_env
  write_systemd_unit
  start_service
  print_summary
}

main "$@"
