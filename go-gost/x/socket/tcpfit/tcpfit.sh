#!/usr/bin/env bash
# tcpfit — 单机 TCP 调优代理
#
# 纯 bash, 除 iperf3(仅 sweep 需要) 外无依赖, 可在任何最小化 VPS 上直接跑.
# 所有"该设多少"的判断都由实测或机器规格推导, 不使用抄来的固定值.
#
# 用法:
#   tcpfit.sh                               交互式菜单（不带参数即可, 推荐; 需要终端）
#   tcpfit.sh detect                        输出机器画像
#   tcpfit.sh probe  --peer HOST            探测可用带宽(虚拟网卡读不到标称值时用)
#   tcpfit.sh tune   [选项]                 应用基础调优
#   tcpfit.sh sweep  --peer HOST [选项]     实测限速器拐点 (-4/-6 指定协议族, 默认 -4)
#                                           预计流量很大时先问一次, 无人值守加 --yes
#   tcpfit.sh shape  --rate N | --off       应用/移除出向整形
#   tcpfit.sh harden --swap 2G              加 swap（小内存机防止进程被杀）
#   tcpfit.sh verify [--peer HOST]          验证当前状态
#   tcpfit.sh status                        显示当前配置
#   tcpfit.sh rollback                      回滚到出厂（= archive restore 0000）
#   tcpfit.sh archive list                  列出所有调优存档
#   tcpfit.sh archive save [名字]           把当前状态存成新存档
#   tcpfit.sh archive restore <序号|名字>   回滚到指定存档
#   tcpfit.sh archive rename <序号> <名字>  改名（0000 不可改）
#   tcpfit.sh archive delete <序号>         删除（0000 不可删）
#   tcpfit.sh uninstall [--keep-archives]   卸载：回滚 + 删配置 + 删自己
#
# 运行计数: 启动时会向 tcpfit.spacevps.cc 发一次匿名计数请求（纯计数, 不含任何
#           机器标识, 只带版本号）, 用于显示"今天多少次 / 累计多少次".
#           关掉:  TCPFIT_NO_TELEMETRY=1   或   touch /var/lib/tcpfit/no-telemetry
#
# 退出码: 0 成功 / 1 参数或环境错误 / 2 实测失败

set -uo pipefail
umask 022   # 固定权限: 生成的脚本和配置不能因为宽松 umask 变成他人可写

VERSION="0.5.9"
STATE_DIR="/var/lib/tcpfit"
SYSCTL_FILE="/etc/sysctl.d/99-tcpfit.conf"
QDISC_SCRIPT="/usr/local/sbin/tcpfit-qdisc.sh"
QDISC_UNIT="/etc/systemd/system/tcpfit-qdisc.service"
ROUTE_HOOK="/etc/networkd-dispatcher/routable.d/50-tcpfit-initcwnd"
# PPPoE/PPP 每次拨通都是新接口, qdisc 和路由窗口跟着消失. 而
# tcpfit-qdisc.service 是 oneshot 只在开机跑一次, networkd-dispatcher
# 又管不到 pppd 拉起的接口 —— 这类机器必须挂 pppd 自己的钩子.
PPP_HOOK="/etc/ppp/ip-up.d/50-tcpfit"
# 路由窗口的第三条持久化路. 只靠 networkd-dispatcher 覆盖不住普通 Debian/Ubuntu
# VPS —— 那个包不是默认装的（实测 hkt1: Debian 12 / systemd / ifupdown, 没有它）.
# 后果很隐蔽: sysctl 和整形都持久, 只有路由窗口重启后丢, 而 status 显示一切正常,
# 用户只会发现"重启后跨海速度掉了一截"却查不出原因.
INITCWND_UNIT="/etc/systemd/system/tcpfit-initcwnd.service"
INITCWND_SCRIPT="/usr/local/sbin/tcpfit-initcwnd.sh"
# systemd-networkd 管的 DHCP 网卡: 窗口直接写进 networkd 自己的配置(drop-in),
# 让它每次装默认路由都自带. 只在路由外面改的话, networkd 一重启就按自己的配置
# 重建路由, 窗口跟着丢 —— 实测本机 9/30 openssl 安全更新后 needrestart 重启了
# networkd, 9/27 设的 32/32 就没了, 钩子也没补回来.
NETWORKD_DIR="/etc/systemd/network"
INITCWND_DROPIN_NAME="50-tcpfit-initcwnd.conf"
BBR_MODULE_FILE="/etc/modules-load.d/tcpfit-bbr.conf"
INITCWND_MARKER="$STATE_DIR/initcwnd.owned"
# 实际写入的窗口值. 钩子重建时要按这个来, 不能硬编码 32 ——
# 恢复存档时窗口可能是 20/24, write_qdisc 再生成一次钩子就把它覆盖掉了.
INITCWND_VALS="$STATE_DIR/initcwnd.vals"
SNAPSHOT="$STATE_DIR/pre-tune.snapshot"
FACTS="$STATE_DIR/facts"

# ── 输出 ────────────────────────────────────────────────────────────────────
# 配色对齐 x-ui, 用户在同一台机器上看到的风格一致
if [ -t 1 ]; then
  green=$'\033[0;32m'; red=$'\033[0;31m'; yellow=$'\033[0;33m'
  blue=$'\033[0;36m';  bold=$'\033[1m';   plain=$'\033[0m'
else
  green=''; red=''; yellow=''; blue=''; bold=''; plain=''
fi
_c(){ [ -t 1 ] && printf '\033[%sm%s\033[0m' "$1" "$2" || printf '%s' "$2"; }
info(){ printf '%s %s\n' "$(_c '0;36' '[*]')" "$*"; }
ok(){   printf '%s %s\n' "$(_c '0;32' '[+]')" "$*"; }
warn(){ printf '%s %s\n' "$(_c '0;33' '[!]')" "$*" >&2; }
# 第二个参数是退出码, 所以消息只能取 $1 —— 用 $* 会把退出码也打进消息里,
# 屏幕上出现 "未做任何改动 1" 这种尾巴.
die(){  printf '%s %s\n' "$(_c '0;31' '[x]')" "$1" >&2; exit "${2:-1}"; }

# 按显示宽度对齐：CJK 占 2 列, printf 的 %-Ns 按字节算会错位.
# 不能依赖 awk 的多字节支持 —— mawk(Debian 默认) 没有, 会把 3 字节的中文算成 3 个字符.
# 这里直接按 UTF-8 前导字节判断：ASCII=1列, 2字节序列=1列, 3字节及以上=2列, 续字节=0列.
_dispw(){
  printf '%s' "$1" | LC_ALL=C od -An -tu1 2>/dev/null | awk '
    {for(i=1;i<=NF;i++){b=$i
       if(b<128)            n++          # ASCII
       else if(b<192)       continue     # 续字节, 不计宽
       else if(b<224)       n++          # 2 字节序列(拉丁扩展等)
       else if(b==226){ nx=$(i+1); if(nx==148||nx==149){ n++; i+=2; continue } n+=2 }
       else                 n+=2         # 3 字节及以上(CJK、全角符号)
    }} END{print n+0}'
}
kv(){ local w; w=$(_dispw "$1"); printf '  %s%*s %s\n' "$1" $(( 20 - w )) "" "$2"; }
# 把字符串按「显示宽度」补齐到 N 列, 供手工排表用
_pad(){  local w; w=$(_dispw "$1"); printf '%s%*s' "$1" $(( $2 - w )) ""; }
_rpad(){ local w; w=$(_dispw "$1"); printf '%*s%s' $(( $2 - w )) "" "$1"; }
# 「确认」和「结果」里的两列排版
_conf(){ printf '      %s %s\n' "$(_pad "$1" 14)" "$2"; }

# 同时跑两个实例会同时抢 qdisc、快照和 sysctl. 用文件锁串行化.
LOCK_FILE="/var/lock/tcpfit.lock"
take_lock(){
  command -v flock >/dev/null || return 0
  mkdir -p "$(dirname "$LOCK_FILE")" 2>/dev/null
  # 注意不能写成 exec 9>FILE 2>/dev/null —— 那个 2>/dev/null 会被 exec 当成
  # 永久重定向, 把整个脚本的 stderr 都吞掉, 所有 die/warn 就都看不见了.
  [ -w "$(dirname "$LOCK_FILE")" ] || return 0
  # 同一次运行里可能调多次(菜单 -> 子命令). 已经拿到过就直接返回,
  # 不要重新 exec 9> —— 那会关掉旧 fd 再开新的, 而继承了旧 fd 的
  # 后台子进程还在持锁, 于是自己把自己挡在外面.
  [ "${LOCK_HELD:-0}" = 1 ] && return 0
  exec 9>"$LOCK_FILE" || return 0
  flock -n 9 && { LOCK_HELD=1; return 0; }

  # 锁被占: 可能真有另一个在跑, 也可能是上次异常退出(SSH 断线/被 kill)卡住了.
  # 给出持有者和已运行时长, 让用户能判断, 并提供一键结束 —— 光说"等它结束"
  # 遇到卡死的情况没有出路. 而且 bash <(curl ...) 起的进程 cmdline 是
  # /dev/fd/63, 用 pkill -f tcpfit 根本找不到它.
  local pids age
  # 排除自己 —— 上面已经 exec 9> 打开了锁文件, 不排掉会把自己也列成持有者
  pids=$(fuser "$LOCK_FILE" 2>/dev/null | tr -s ' ' | tr ' ' '\n' | grep -vx "$$" | grep -x '[0-9]*' | tr '\n' ' ')
  [ -n "$pids" ] || pids=$(command -v lsof >/dev/null && lsof -t "$LOCK_FILE" 2>/dev/null | grep -vx "$$" | tr '\n' ' ')
  warn "另一个 tcpfit 正在运行（锁: $LOCK_FILE）"
  if [ -n "$pids" ]; then
    echo "      持有者:"
    for _p in $pids; do
      age=$(ps -o etime= -p "$_p" 2>/dev/null | tr -d ' ')
      [ -n "$age" ] && printf '        PID %-8s 已运行 %s\n' "$_p" "$age"
    done
  fi
  echo "      跑得太久多半是上次异常退出卡住了."
  echo
  # 拿不到 PID 就没法安全地只杀它们, 不如让用户自己处理
  [ -n "$pids" ] || die "查不到锁的持有者, 手动检查: fuser -v $LOCK_FILE"
  if confirm "  结束它并继续？" n; then
    exec 9>&-                                   # 先松开自己, 否则会把自己一起杀掉
    # 只杀最初记录的那几个 PID. 绝不能第二次去查锁文件 ——
    # 旧实例收到 TERM 退出后, 别的新实例可能在这 3 秒里拿到锁,
    # 再查一次就会把那个无辜的新实例 KILL 掉(实测复现过, 新实例退出码 137).
    kill -TERM $pids 2>/dev/null            # 先 TERM, 让对方的 trap 有机会恢复 qdisc
    sleep 3
    for _p in $pids; do
      kill -0 "$_p" 2>/dev/null && kill -KILL "$_p" 2>/dev/null
    done
    sleep 1
    reap_iperf
    exec 9>"$LOCK_FILE" || return 0
    flock -n 9 || die "锁仍被占用, 手动查看: fuser -v $LOCK_FILE"
    ok "已结束, 继续"
    return 0
  fi
  die "已取消"
}

need_root(){ [ "$(id -u)" = 0 ] || die "需要 root 权限"; }

# 转圈. 长操作(iperf3 一跑十几秒)不给反馈的话用户会以为卡死了.
# 非交互环境(管道/日志)不画, 避免把日志刷满控制字符.
SPIN_FRAMES='⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏'
spin_wait(){   # spin_wait <pid> <描述>
  local pid="$1" msg="$2" i=0
  if [ ! -t 2 ]; then wait "$pid" 2>/dev/null; return $?; fi
  while kill -0 "$pid" 2>/dev/null; do
    printf '\r  \033[0;36m%s\033[0m %s' "${SPIN_FRAMES:$((i++%10)):1}" "$msg" >&2
    sleep 0.12
  done
  printf '\r\033[K' >&2
  wait "$pid" 2>/dev/null
}
# ── 流量计量 ──────────────────────────────────────────────────────────────
# 读网卡字节计数器. 比按速率估算准 —— 它把重传、协议开销、握手全算进去了.
TRAFFIC_RX0=""; TRAFFIC_TX0=""
traffic_mark(){
  local i; i=$(detect_iface)
  TRAFFIC_RX0=$(cat "/sys/class/net/$i/statistics/rx_bytes" 2>/dev/null || echo 0)
  TRAFFIC_TX0=$(cat "/sys/class/net/$i/statistics/tx_bytes" 2>/dev/null || echo 0)
}
traffic_report(){
  [ -n "$TRAFFIC_TX0" ] || return 0
  local i rx tx drx dtx; i=$(detect_iface)
  rx=$(cat "/sys/class/net/$i/statistics/rx_bytes" 2>/dev/null || echo 0)
  tx=$(cat "/sys/class/net/$i/statistics/tx_bytes" 2>/dev/null || echo 0)
  drx=$(( rx - TRAFFIC_RX0 )); dtx=$(( tx - TRAFFIC_TX0 ))
  [ "$drx" -lt 0 ] && drx=0; [ "$dtx" -lt 0 ] && dtx=0
  echo
  printf '  %s本次测试消耗流量%s\n' "$bold" "$plain"
  rule
  awk -v tx="$dtx" -v rx="$drx" '
    function h(b){ if(b>=1073741824) return sprintf("%.2f GB", b/1073741824); return sprintf("%.0f MB", b/1048576) }
    BEGIN{
      printf "  %-16s %s\n","出向 (上传)", h(tx)
      printf "  %-16s %s\n","入向 (下载)", h(rx)
      printf "  %-16s %s\n","双向合计", h(tx+rx)
    }'
  rule
}

rule(){ printf '  \033[2m%s\033[0m\n' "────────────────────────────────────────────────"; }
step(){ printf '\n  \033[1;36m▸ %s\033[0m\n' "$*"; }

# 用 bash <(curl ...) 一条命令跑时, $0 是临时 fd, 脚本一退出就没了.
# 这里把自己装到系统里, 以后想回滚/查状态还能找到.
# 测速走哪个协议族. 默认 IPv4 —— 双栈机器上 v4 和 v6 到同一个对端的延迟可能差很多,
# 实测见过同城对端 v4 0.8ms / v6 93ms, 按 v6 的 RTT 选对端会把最好的那个判成"太远".
# 更麻烦的是 ping 和 iperf3 各自独立解析, 可能一个走 v4 一个走 v6 ——
# 那样挑选依据和实际测量根本不是同一条链路.
IP_FAMILY="${IP_FAMILY:--4}"

# 按当前协议族把主机名解析成字面地址. bash 的 /dev/tcp 没法指定协议族,
# 只能先解析好再连. 注意 v6 字面量不能加方括号, bash 认不了.
#
# -6 那支必须滤掉 ::ffff: 开头的 v4 映射地址 —— getent ahostsv6 对只有 A 记录的
# 主机也会返回结果(如 ::ffff:20.205.243.166), 而 iperf3 -6 连这种地址还会成功.
# 不滤的话: 用户选了 v6, 整个测试悄悄跑在 IPv4 上, 一句提示都没有.
resolve_ip(){   # resolve_ip <主机名>
  case "$IP_FAMILY" in
    -6) getent ahostsv6 "$1" 2>/dev/null | awk '/STREAM/ && $1 !~ /^::ffff:/ {print $1; exit}' ;;
    *)  getent ahostsv4 "$1" 2>/dev/null | awk '/STREAM/{print $1; exit}' ;;
  esac
}
# 端口探测: 解析不出对应协议族的地址就直接算不可达
probe_port(){   # probe_port <主机> <端口> [超时秒]
  local ip; ip=$(resolve_ip "$1"); [ -n "$ip" ] || return 1
  timeout "${3:-6}" bash -c "cat < /dev/null > /dev/tcp/${ip}/${2}" 2>/dev/null
}

# 对端 iperf3 实例的端口范围. Leaseweb/OVH 开 5201-5210, Clouvider 开 5200-5209 ——
# 所以 5200 也得在表里. 放末尾: 放开头会让 16 个 Leaseweb/OVH 节点每次都先白撞一下.
PORT_POOL="5201 5202 5203 5204 5205 5206 5207 5208 5209 5210 5200"

# 把首选端口排到表最前面, 其余保持原序. run_iperf 和选对端共用一份顺序.
port_order(){   # port_order <首选端口>
  local p out="$1"
  for p in $PORT_POOL; do [ "$p" = "$1" ] || out="$out $p"; done
  echo "$out"
}

# 选对端时的预检端口. 只探 5201 会出大事 —— 5201 是 iperf3 默认端口, 有机房
# 专门封它防测速滥用. 实测过一台客户机器(Debian 13, 依赖齐全, 无本地防火墙):
# 出站 5201 被单独封死(对 6 个不同目标 0/5), 而 5200/5202/5210/5211/6201 全是 5/5,
# 结果 18 个节点全被判成 "port closed", 工具完全不可用, 最后那句
# "公共测速服务器暂时都不可用" 还把责任推给了完全无辜的对端.
PROBE_PORTS="5201 5202 5203 5200"
PROBE_HIT=""        # 上一个探通的端口. 出站封锁对所有节点一致, 记住能省掉 17 次重复失败
PROBE_PORT_OK=""    # probe_peer_port 的结果

# 不能用 $(...) 取结果 —— 命令替换是子 shell, PROBE_HIT 记不住, 缓存就失效了.
probe_peer_port(){  # probe_peer_port <主机>  -> 成功则 PROBE_PORT_OK=端口
  local try seen=""
  PROBE_PORT_OK=""
  for try in $PROBE_HIT $PROBE_PORTS; do
    case " $seen " in *" $try "*) continue ;; esac      # PROBE_HIT 可能和表里重复
    seen="$seen $try"
    probe_port "$1" "$try" 4 || continue
    PROBE_HIT="$try"; PROBE_PORT_OK="$try"; return 0
  done
  return 1
}

# ⚠ 本文件顶部是 `set -uo pipefail`, 所以【绝对不能写 `命令 | grep -q 模式`】.
# grep -q 一匹配就立刻退出并关掉管道, 写端还没写完就吃 SIGPIPE 死掉,
# pipefail 把 141 当成整条管道的返回值 —— 于是"匹配到了"被读成"没匹配到".
# 只要匹配点之后还有内容要写就会触发. 2026-08-11 客户机实测:
#   ip -4 addr show scope global | grep -q 'inet '   →  292/300 返回 141
# 那台装了 docker, eth0 后面还跟着 docker0; 没有 docker 的机器 eth0 就是最后一个,
# ip 写完了 grep 才退出, 于是永远不复现 —— 同款机器一台好一台坏, 全卡在这里.
# 统一改成命令替换 + case: 读到 EOF, 写端永远不会被打断.
has_str(){  case "$1" in *"$2"*) return 0 ;; *) return 1 ;; esac; }
starts_with(){ case "$1" in "$2"*) return 0 ;; *) return 1 ;; esac; }   # 等价 grep -q '^…'
# 等价 grep -qw: 匹配处前后都不能是单词字符. 不能简单按空格切 ——
# 实测 "reno,cubic,bbr" 和 tab 分隔的列表 grep -qw 都算命中, 按空格切会漏判.
has_word(){ case "$1" in
    "$2"|"$2"[!A-Za-z0-9_]*|*[!A-Za-z0-9_]"$2"|*[!A-Za-z0-9_]"$2"[!A-Za-z0-9_]*) return 0 ;;
    *) return 1 ;; esac; }
# 等价 grep -q . : 只要有【任意非换行字符】就算有内容. 用 [![:space:]] 会把
# 纯空格的输出判成空, 和原来的行为不一致.
not_blank(){ case "$1" in *[!$'\n']*) return 0 ;; *) return 1 ;; esac; }
# 取第一行, 空则给默认值. 不用 `| head -1 || 默认值` —— head 读够一行就退出,
# 写端(grep)还有匹配要输出时被 SIGPIPE 打死, pipefail 返回 141, `||` 于是误触发,
# 结果是「真值 + 默认值」一起打出来.
first_or(){ local s="${1%%$'\n'*}"; if [ -n "$s" ]; then printf '%s' "$s"; else printf '%s' "$2"; fi; }

# tc 显示速率时【只在除得尽的时候才换单位】. 2026-08-11 两台机器实测 (iproute2 5.15):
#   999→999Mbit  1000→1Gbit  1001→1001Mbit  1500→1500Mbit
#   2000→2Gbit   2500→2500Mbit  3000→3Gbit  5000→5Gbit  9171→9171Mbit  10000→10Gbit
# 所以 "2.5Gbit"/"9.171Gbit" 这种带小数的显示【不会出现】——
# 曾经的注释和一份 review 都这么写过, 是错的, 别再照抄.
# 真正会踩坑的是【整千值】: 1000/2000/3000/5000/10000, 它们显示成 "NGbit".
# 所有读取整形值的地方都必须走这个函数, 不许各写各的正则. 早期 status / verify /
# banner / 向导结果四处各写死 'rate [0-9]+[MKG]bit' + "${shaper%Mbit}", 后果是
# "1Gbit" 剥不掉 "Gbit" → 当成非数字 → 达成率判断被跳过, 还反过来提示
# "这台没有应用整形"（`tcpfit shape --rate 1000` 对 1G 口是最自然的手输值）.
# 小数分支保留是为了防御: 换个 tc 版本万一真打出小数, 这里也算得对.
# 返回 Mbit 数字; 没有整形时返回空并且退出码非 0.
tc_rate_mbit(){   # tc_rate_mbit "<tc class show 的输出>"
  local r
  r=$(grep -oE 'rate [0-9.]+[KMGTkmgt]?bit' <<<"${1:-}")
  r=${r%%$'\n'*}          # 只要第一条; 不用 `| head -1`, 见上面 SIGPIPE 那段注释
  r=${r#rate }
  [ -n "$r" ] || return 1
  awk -v s="$r" 'BEGIN{
    u = s; sub(/^[0-9.]+/, "", u); sub(/bit$/, "", u)
    v = s + 0
    if      (u == "K" || u == "k") v = v/1000
    else if (u == "G" || u == "g") v = v*1000
    else if (u == "T" || u == "t") v = v*1000000
    else if (u == "")              v = v/1000000
    if (v == int(v)) printf "%d", v; else printf "%g", v }'
}

# 本机有没有可用的 IPv4 出网能力. 纯 v6 机器要自动走 v6, 不能傻等 v4 超时.
have_ipv4(){
  local rt ad
  rt=$(ip -4 route show default 2>/dev/null)
  ad=$(ip -4 addr show scope global 2>/dev/null)
  not_blank "$rt" && has_str "$ad" 'inet ' && return 0
  # 默认路由不一定叫 "default". 机器上跑着 VPN/透明代理(WireGuard、sing-box、Clash TUN)时,
  # 全局路由常被拆成 0.0.0.0/1 + 128.0.0.0/1, 或整个挪进策略路由的独立表 ——
  # 这两种情况 `route show default` 都是空的, 而机器的 v4 明明是通的.
  # `route get` 走内核真正的选路逻辑, 拆分路由和策略表都算数; 输出里有 src
  # 就说明既选得出出口、也有全局源地址, 一次覆盖原来那两个条件.
  has_str "$(ip -4 route get 1.1.1.1 2>/dev/null)" ' src '
}

# 本机有没有可用的 IPv6 出网能力. 光有地址不算 —— 很多机器配了 v6 地址但没路由.
have_ipv6(){
  local rt ad
  rt=$(ip -6 route show default 2>/dev/null)
  ad=$(ip -6 addr show scope global 2>/dev/null)
  not_blank "$rt" && has_str "$ad" 'inet6' && return 0
  has_str "$(ip -6 route get 2606:4700:4700::1111 2>/dev/null)" ' src '
}

PEER_PORT="${PEER_PORT:-5201}"   # 选定对端时确定的可用端口
WIZARD=0                         # 一键流程内为 1：子命令只输出执行日志, 收尾统一由 wizard 打印
# 装成不带扩展名的 tcpfit, 放 /usr/local/bin —— 用户敲 `tcpfit` 就能进菜单.
# 不用 /usr/local/sbin 是因为它不在普通用户的 PATH 里, 非 root 敲命令会「找不到命令」,
# 而不是看到「需要 root 权限」这个有用的提示.
SELF_PATH="/usr/local/bin/tcpfit"
LEGACY_SELF="/usr/local/sbin/tcpfit.sh"   # v0.3.1 及更早装在这里, 装新版时清掉
# 面向用户的提示一律用这个, 不能用 $0 ——
# bash <(curl ...) 跑时 $0 是 /dev/fd/63, 提示出来的命令用户根本没法执行
# 提示用户"下一步敲什么". 装好之后 tcpfit 在 PATH 里, 直接说命令名即可；
# 没装成（非 root / 没 curl）才退回完整路径. 绝不能用 $0 ——
# bash <(curl ...) 跑时 $0 是 /dev/fd/63, 提示出来的命令用户根本没法执行.
disp(){
  [ -x "$SELF_PATH" ] && { echo "tcpfit"; return; }
  case "$0" in /dev/fd/*|/proc/self/fd/*|bash|-bash) echo "$SELF_PATH" ;; *) echo "$0" ;; esac
}
# 装到系统里的那一份, 必须和「你刚跑的这一份」是同一个版本.
#
# 原先无条件拉 main：你按 v0.3.0 下载、校验、运行, 它转头把 main 装进
# /usr/local/sbin —— 之后每次敲 tcpfit.sh 跑的都是没校验过的代码,
# 固定版本的意义被完全抵消. （我自己踩过：推完新版去远端验证, 看到的还是旧菜单.)
#
# 为什么不能直接复制"正在运行的脚本"：bash <(curl ...) 时 $0 是 /dev/fd/63,
# 内容已被 bash 读走, 再 cat 只能读到 0 字节；curl | bash 时 $0 = bash, 根本不可读.
# 实测验证过这两种情况. 所以只能按版本号回拉, 并校验拉到的确实是同一版.
SELF_URL="https://raw.githubusercontent.com/Kylin010/tcpfit/v${VERSION}/tcpfit.sh"
self_install(){
  [ "$(id -u)" = 0 ] || return 0
  case "$0" in "$SELF_PATH") return 0 ;; esac      # 已经是装好的那份
  command -v curl >/dev/null || return 0
  curl -fsSL "$SELF_URL" -o "$SELF_PATH".tmp 2>/dev/null || return 0
  # 校验版本一致. 开发期 main 领先 tag 时这里会失败, 跳过安装也是对的.
  if [ -s "$SELF_PATH".tmp ] && starts_with "$(head -1 "$SELF_PATH".tmp 2>/dev/null)" '#!' \
     && grep -q "^VERSION=\"$VERSION\"" "$SELF_PATH".tmp; then
    mv "$SELF_PATH".tmp "$SELF_PATH"; chmod +x "$SELF_PATH"
    rm -f "$LEGACY_SELF"                      # 清掉旧位置, 免得两份不同版本并存
    ok "Installed: run 'tcpfit' anytime"
  else
    rm -f "$SELF_PATH".tmp
  fi
}

# ── 从旧名字 nettune 迁移 ──────────────────────────────────────────────────
# 项目 v0.3.1 从 nettune 改名为 tcpfit. 老机器上所有产物的文件名都还是 nettune-*,
# 新脚本按新名字去找会一个都找不到 —— 最危险的是 take_snapshot 的保护：
# 它检查 $SYSCTL_FILE 是否存在, 改名后该变量指向新路径, 老文件在它眼里不存在,
# 于是把「已调优状态」当成出厂基线存进快照, rollback 从此永久错误且无任何报错.
# 所以必须先搬迁, 而不是假装老部署不存在.
migrate_legacy(){
  local old_state=/var/lib/nettune
  local old_sysctl=/etc/sysctl.d/99-nettune.conf
  local old_qdisc=/usr/local/sbin/nettune-qdisc.sh
  local old_unit=/etc/systemd/system/nettune-qdisc.service
  local old_hook=/etc/networkd-dispatcher/routable.d/50-nettune-initcwnd
  local old_mod=/etc/modules-load.d/nettune-bbr.conf
  local old_self=/usr/local/sbin/nettune.sh
  # 一个旧产物都没有 → 全新机器, 什么都不用做
  [ -e "$old_state" ] || [ -e "$old_sysctl" ] || [ -e "$old_unit" ] || return 0
  [ "$(id -u)" = 0 ] || return 0

  info "检测到旧版本(nettune)的部署, 正在迁移到新名字(tcpfit)…"
  local rate=""
  # 先把整形值抠出来, 后面用新名字重建；不能直接改文件名, unit 里的路径也要跟着变
  [ -f "$old_qdisc" ] && rate=$(grep -oE 'rate [0-9]+mbit' "$old_qdisc" | head -1 | grep -oE '[0-9]+')
  systemctl disable --now nettune-qdisc.service >/dev/null 2>&1
  rm -f "$old_unit" "$old_qdisc"; systemctl daemon-reload >/dev/null 2>&1

  # 逐文件搬, 不搬目录 —— mv -n 在目标目录已存在时会变成 STATE_DIR/nettune/,
  # 快照就找不到了; 而后面的 rm -rf 还可能把原数据删掉
  if [ -d "$old_state" ]; then
    mkdir -p "$STATE_DIR"
    for _f in "$old_state"/*; do
      [ -e "$_f" ] || continue
      [ -e "$STATE_DIR/$(basename "$_f")" ] || mv "$_f" "$STATE_DIR/"
    done
    rmdir "$old_state" 2>/dev/null || warn "旧目录 $old_state 非空, 已保留"
  fi
  [ -f "$old_sysctl" ] && mv -f "$old_sysctl" "$SYSCTL_FILE"
  [ -f "$old_hook" ]   && mv -f "$old_hook" "$ROUTE_HOOK"
  [ -f "$old_mod" ]    && mv -f "$old_mod" /etc/modules-load.d/tcpfit-bbr.conf
  rm -f "$old_self" "$LEGACY_SELF"

  if [ -n "$rate" ]; then
    write_qdisc "$rate" "$(detect_iface)"
    systemctl restart tcpfit-qdisc.service 2>/dev/null || "$QDISC_SCRIPT" >/dev/null 2>&1
    ok "迁移完成, 整形 ${rate}Mbit 已用新名字重建"
  else
    ok "迁移完成"
  fi
  info "快照保留在 $SNAPSHOT, rollback 仍然可用."
}

# ── 环境检测 ────────────────────────────────────────────────────────────────
# 默认路由网卡. 【不跟 $IP_FAMILY 走】—— 网卡是物理概念, 整形和 qdisc 打在同一张卡上,
# 选 v4 还是 v6 测速都是它. 只是纯 v6 机器的 v4 路由表是空的, 所以 v4 查不到时回退查 v6.
# (`ip route` 等价于 `ip -4 route`, 早期版本只写这一句, 纯 v6 机器直接
#  die "找不到默认路由网卡", 从来就没跑起来过.)
# 从默认路由里按【关键字】取字段, 绝不能按位置数.
#   default via 10.0.0.1 dev eth0 proto dhcp   -> $5 = eth0   碰巧对
#   default dev ppp0 scope link                -> $5 = link   错
# 点对点链路(PPPoE / PPP / 部分静态路由)的默认路由没有 `via <IP>` 这两个词,
# 整行左移两位. 实测客户的 HKT PPPoE 机器: detect_iface 返回 "link",
# 于是 qdisc / 整形 / MTU / 扫描全部作用在一个不存在的网卡上, 工具整体不可用.
# 触发条件是「默认路由没有 via」, 不限 PPPoE —— 静态点对点路由同样会中.
# 去掉前导零. "08" 过得了整数检查, 但 bash 算术按八进制解析会报
# "value too great for base" —— 统一成十进制再往下传.
strip_zeros(){
  local v="$1"
  case "$v" in ''|*[!0-9]*) printf '%s' "$v"; return ;; esac
  while [ "${#v}" -gt 1 ] && [ "${v#0}" != "$v" ]; do v=${v#0}; done
  printf '%s' "$v"
}

route_field(){   # route_field <关键字> [路由行]
  local key="$1" line="${2-}"
  [ $# -ge 2 ] || line=$(ip -4 route show default 2>/dev/null | head -1)
  printf '%s\n' "$line" | awk -v k="$key" '{
    for(i=1;i<NF;i++) if($i==k){print $(i+1); exit}}'
}

# 设 initcwnd 必须【沿用现有默认路由的全部 token】, 只替换窗口字段.
# 自己拼 `via $gw dev $if` 有两个问题: 丢掉 scope/metric/proto/onlink 等
# 服务商下发的属性; 点对点路由压根没有 via, 拼不出来 —— 早期版本因此
# 整块跳过 initcwnd, PPPoE 机器一直拿不到.
# 清掉【当前所有】默认路由上的窗口字段, 并读回核实.
# 回滚不能照快照里那条路由原样 replace 回去: 网络重配之后 metric 可能变了,
# 而 metric 是路由的键 —— replace 会【新建】一条而不是替换.
# 实测: 快照 metric 42, 当前 metric 10 且带 32/32, rollback 返回 0,
# 结果留下两条默认路由, 高优先级那条仍是 32/32, 另外多一条过期的 metric 42.
# initcwnd 回滚的准确逆操作就是"把窗口字段摘掉", 不是"塞回旧路由".
route_restore_windows(){   # route_restore_windows <网卡> [要恢复的窗口 token...]
  # 回滚窗口要做三件事, 少一件就出问题:
  #   1) 保留当前路由的网关 / metric / proto / src —— 照搬快照那条会新建一条
  #      过期路由（metric 是键）, 留下两条默认路由.
  #   2) 把【快照里原有的窗口值】写回去, 不是一律清空 —— 用户调优前可能自己
  #      设过 initcwnd 20 initrwnd 24, 无条件摘掉等于丢了他的基线.
  #   3) 只动目标网卡那条, 不碰无关路由.
  local iface="$1"; shift
  local -a want=("$@")
  local line t skip rc=0 touched=0
  local -a args clean
  while IFS= read -r line; do
    [ -n "$line" ] || continue
    # 只处理目标网卡那条（快照没记网卡时对所有默认路由生效）
    if [ -n "$iface" ]; then
      case " $line " in *" dev $iface "*) ;; *) continue ;; esac
    fi
    args=(); clean=(); skip=0
    read -r -a args <<< "$line"
    for t in "${args[@]}"; do
      if [ "$skip" = 1 ]; then skip=0; continue; fi
      case "$t" in initcwnd|initrwnd) skip=1 ;; *) clean+=("$t") ;; esac
    done
    [ "${#clean[@]}" -gt 1 ] || continue
    touched=1
    ip -4 route replace "${clean[@]}" "${want[@]+"${want[@]}"}" 2>/dev/null || rc=1
  done <<< "$(ip -4 route show default 2>/dev/null)"
  [ "$touched" = 1 ] || return 1      # 没有匹配的当前路由, 交给调用方兜底
  # 读回核实【只能看目标那条路由】. 早期版本把所有默认路由拼在一起检查,
  # 于是另一块网卡上用户自己设的 initcwnd 20 会让校验失败 ——
  # 明明 test0 清对了、test1 的自定义值也正确保留, 却报"窗口未完全还原"并返回 1.
  local now
  now=$(ip -4 route show default 2>/dev/null)
  if [ -n "$iface" ]; then
    now=$(printf '%s\n' "$now" | awk -v d="$iface" '{
      for(i=1;i<NF;i++) if($i=="dev" && $(i+1)==d){print; next}}')
  fi
  if [ "${#want[@]}" = 0 ]; then
    case "$now" in *initcwnd*|*initrwnd*) rc=1 ;; esac
  else
    case "$now" in *"${want[*]}"*) ;; *) rc=1 ;; esac
  fi
  return "$rc"
}

route_set_initcwnd(){   # route_set_initcwnd <值>
  local n="$1" route token skip=0
  local -a args=() clean=()
  route=$(ip -4 route show default 2>/dev/null | head -1)
  [ -n "$route" ] || return 1
  # 多路径路由长这样, 第一行既没有 dev 也没有 via:
  #   default proto static
  #       nexthop via 10.1.1.1 dev v1 weight 1
  #       nexthop via 10.2.2.1 dev v2 weight 1
  # 拿第一行去 replace 会试图把整条多路径换成一条无出口的路由.
  # 内核会拒(No such device, 实测), 但别指望内核兜底 —— 自己先认出来.
  [ -n "$(route_field dev "$route")" ] || return 1
  read -r -a args <<< "$route"
  for token in "${args[@]}"; do
    if [ "$skip" = 1 ]; then skip=0; continue; fi
    case "$token" in initcwnd|initrwnd) skip=1 ;; *) clean+=("$token") ;; esac
  done
  [ "${#clean[@]}" -gt 1 ] || return 1
  ip -4 route replace "${clean[@]}" initcwnd "$n" initrwnd "$n" 2>/dev/null
}

# 出口网卡. 顺序很重要:
#   1) 有明确测速目标 -> 按目标 `ip route get`. 多出口机器上主表的 default
#      未必是目标走的路: 实测 default 出 test0, 而 198.51.100.1 经 table 100
#      出 test1 —— 按 default 选就会在错误的网卡上测速和整形.
#   2) 主表 default（v4, 再 v6）
#   3) 没有 default 也能有出口: 策略路由(default 在 table 100)、
#      双 /1 路由(0.0.0.0/1 + 128.0.0.0/1 覆盖全网, 压根没有 default).
#      早期版本到这里就 die "找不到默认路由网卡", 这类机器整个用不了.
detect_iface(){
  local i t
  t="${TCPFIT_ROUTE_TARGET:-}"
  if [ -n "$t" ]; then
    i=$(route_field dev "$(ip "${IP_FAMILY:--4}" route get "$t" 2>/dev/null | head -1)")
    [ -n "$i" ] && { echo "$i"; return; }
  fi
  i=$(route_field dev)
  [ -n "$i" ] || i=$(route_field dev "$(ip -6 route show default 2>/dev/null | head -1)")
  [ -n "$i" ] || i=$(route_field dev "$(ip -4 route get 1.1.1.1 2>/dev/null | head -1)")
  [ -n "$i" ] || i=$(route_field dev \
        "$(ip -6 route get "${TCPFIT_ROUTE_TARGET6:-2606:4700:4700::1111}" 2>/dev/null | head -1)")
  echo "$i"
}

# 把对端解析成字面地址存进 TCPFIT_ROUTE_TARGET, 供 detect_iface 按目标选出口.
# `ip route get` 只认地址不认域名, 所以必须先解析; 解析不出就不设,
# detect_iface 自然退回按 default 找 —— 不能因为 DNS 失败就让整个流程死掉.
set_route_target(){   # set_route_target <对端>
  local a
  [ -n "${1:-}" ] || return 0
  # `|| a=""` 不能省: 解析失败时命令替换的退出码会传给赋值语句,
  # 调用方开了 set -e 就会在这里中止. 产品自己没开 set -e, 但不能靠这个
  # —— 测试 harness 是开的, 而且这种隐式依赖一换环境就炸（已踩三次）.
  a=$(resolve_ip "$1" 2>/dev/null) || a=""
  [ -n "$a" ] && TCPFIT_ROUTE_TARGET="$a"
  return 0
}
# 网关【只取 v4】. 它唯一的用途是 `ip route replace default via $gw ...`(设 initcwnd),
# 那是 IPv4 路由表操作, 喂 v6 地址进去会直接报
# "Error: inet address is expected rather than 2a0f:...". 实测验证过.
# 纯 v6 机器上这里返回空, 调用方的 [ -n "$gw" ] 会跳过 initcwnd —— 安全降级.
# 点对点链路没有网关, 这里返回空 —— 调用方必须能处理"无网关"而不是当失败.
detect_gw(){    route_field via; }

# 只清理 tcpfit 自己写入的 initcwnd/initrwnd. 旧版本没有 ownership marker,
# 所以兼容两种证据: tcpfit 的持久化 hook, 或快照明确显示调优前没有这两个属性.
# 重建路由时沿用 `ip route show` 的全部 token, 只剔除窗口字段，避免丢掉
# metric/proto/src/onlink 等服务商下发的属性.
clear_owned_initcwnd(){
  local route routes before owned=0 skip=0 token
  local -a args clean
  INITCWND_CLEARED=0
  routes=$(ip -4 route show default 2>/dev/null)
  route=${routes%%$'\n'*}

  [ -f "$INITCWND_MARKER" ] && owned=1
  [ -f "$ROUTE_HOOK" ] && owned=1
  local _d
  for _d in "$NETWORKD_DIR"/*.network.d/"$INITCWND_DROPIN_NAME"; do
    [ -e "$_d" ] && owned=1
  done
  if [ "$owned" = 0 ] && [ -f "$SNAPSHOT" ]; then
    before=$(awk '/^# route: /{sub(/^# route: /, ""); print; exit}' "$SNAPSHOT")
    if [ -n "$before" ] && ! has_str "$before" ' initcwnd ' && \
       ! has_str "$before" ' initrwnd ' && \
       { has_str "$route" ' initcwnd 32' || has_str "$route" ' initrwnd 32'; }; then
      owned=1
    fi
  fi
  [ "$owned" = 1 ] || return 0
  INITCWND_CLEARED=1

  # 先移除持久化入口；即使运行时路由暂时改不了，重连/重启后也不会再写回 32.
  # 单元也要停: 留着的话下次开机它照样把窗口写回去, --no-initcwnd 等于没生效;
  # networkd 的 drop-in 同理, 留着的话 networkd 下次装路由又带上 32.
  remove_initcwnd_persistence || true
  rm -f "$INITCWND_MARKER" "$INITCWND_VALS"
  # PPP 钩子【不能】一并删: 它同时负责重拨后重建整形, 而用户这次只是要求
  # 关掉窗口调整（--no-initcwnd / 小带宽路径）. 删了的话下次重拨整形就没了.
  # 不用重建 —— 钩子里的窗口那段本来就由 [ -f "$INITCWND_MARKER" ] 兜着,
  # 标记一删它自己就退化成"只恢复整形". 所以只在没有整形时才删钩子.
  [ -x "$QDISC_SCRIPT" ] || rm -f "$PPP_HOOK"
  if ! has_str "$route" ' initcwnd ' && ! has_str "$route" ' initrwnd '; then
    return 0
  fi

  read -r -a args <<<"$route"
  for token in "${args[@]}"; do
    if [ "$skip" = 1 ]; then skip=0; continue; fi
    case "$token" in
      initcwnd|initrwnd) skip=1 ;;
      *) clean+=("$token") ;;
    esac
  done
  [ "${#clean[@]}" -gt 0 ] || return 1
  ip -4 route replace "${clean[@]}" 2>/dev/null
}

# 算 BDP 用的 RTT. 固定 150ms, 不再探测.  用 --rtt 可以覆盖.
#
# 为什么不测了 —— 旧做法是 ping 五个国内 DNS 取中位数, 三个问题让它没法用:
#
#  1. anycast 污染. 五个目标里腾讯/百度/CNNIC 三个是 anycast, 会命中就近节点.
#     本机(香港)实测: 2ms / 1ms / 1ms, 而真·国内是 138-145ms —— 中位数取出 2ms,
#     差 70 倍. 更糟的是 BDP 算小之后缓冲区落到 4MB 下限, 而 4MB 正好等于
#     Linux 出厂值, 等于"调了个寂寞", 还打印一份看着完全正常的推导过程.
#  2. 硬依赖 ping + ICMP. 精简镜像不带 iputils-ping, 有的机房挡 ICMP ——
#     两种情况都让 detect_rtt 返回空, 然后 die "无法确定 RTT, 请用 --rtt 指定",
#     而向导里根本没地方填这个参数, 报错把用户指向死路. 客户真踩过.
#  3. 就算测准了也没意义. "到中国的 RTT"不是一个数: 同一台机器同一时刻实测
#     移动 55ms / 联通 93ms / 电信 138ms / 上海电信 145ms, 差 2.6 倍,
#     再叠加晚高峰. 测出来的只是这个分布里随机的一个点.
#
# 为什么是 150 —— 它覆盖常见的跨境代理路径, 再给 socket 2×BDP + 2MB:
#     优化线 40-70ms / 香港普通线 145ms / 美西 160-180ms / 欧美 230-250ms /
#     晚高峰拥塞 300ms 都不会按某次不可靠的 ping 把缓冲区算得特别小.
# 这不是承诺单流全速覆盖到 300ms: tcp_adv_win_scale=1 会为协议和应用预留
# 一部分 socket 空间, 实际可通告窗口还受内核记账、路径和对端共同影响.
# 再提高默认估值收益有限, 而且
#     小内存机器早被 RAM/32 封顶接住(512MB→16MB), 估多高结果都一样;
#     大机器上则要多付 BBR 超发的账 —— 实测超配 215 倍时掉 22% 吞吐.
# 估低才是真危险: 估 40 时缓冲区会算得过小, 2G 口到美西只剩 941 Mbps(47%),
#     而且是硬天花板, 用户怎么测都上不去还查不出原因.
DEFAULT_RTT=150

detect_ram_mb(){ awk '/^MemTotal:/{printf "%d", $2/1024}' /proc/meminfo; }
detect_cores(){  nproc 2>/dev/null || echo 1; }

# 网卡标称速率. 虚拟网卡多半读不到, 返回空由调用方处理
detect_link_mbps(){
  local i="$1" s
  s=$(cat "/sys/class/net/$i/speed" 2>/dev/null)
  [[ "$s" =~ ^[0-9]+$ ]] && [ "$s" -gt 0 ] && echo "$s" || echo ""
}

cmd_detect(){
  local iface rtt ram cores link virt kern cc_avail queues
  iface=$(detect_iface); [ -n "$iface" ] || die "找不到默认路由网卡"
  rtt="$DEFAULT_RTT"; ram=$(detect_ram_mb); cores=$(detect_cores)
  link=$(detect_link_mbps "$iface")
  # systemd-detect-virt 在裸机上输出 none 但退出码为 1, 不能用 || 兜底
  virt=$(systemd-detect-virt 2>/dev/null); [ -n "$virt" ] || virt=unknown
  kern=$(uname -r)
  cc_avail=$(sysctl -n net.ipv4.tcp_available_congestion_control 2>/dev/null)
  queues=$(ls -d /sys/class/net/"$iface"/queues/rx-* 2>/dev/null | wc -l)

  echo "── Machine profile ──"
  kv "Interface"   "$iface"
  kv "Driver"      "$(ethtool -i "$iface" 2>/dev/null | awk -F': ' '/^driver/{print $2}')"
  kv "RX queues"   "$queues"
  kv "Link speed"  "${link:-n/a (virtual NIC)}"
  kv "Kernel"      "$kern"
  kv "Virt"        "$virt"
  kv "CPU cores"   "$cores"
  kv "Memory MB"   "$ram"
  kv "RTT (assumed)" "${rtt}ms  — 固定值, 用于估算 TCP 缓冲区; 要改用 tune --rtt"
  kv "CC available" "$cc_avail"
  kv "BBR"         "$(has_word "$cc_avail" bbr && echo 是 || (modprobe tcp_bbr 2>/dev/null && echo '是(需加载模块)' || echo 否))"

  mkdir -p "$STATE_DIR"
  cat > "$FACTS" <<EOF
IFACE=$iface
RTT_MS=${rtt:-0}
RAM_MB=$ram
CORES=$cores
LINK_MBPS=${link:-0}
KERNEL=$kern
VIRT=$virt
EOF
}

# 数值参数校验. 所有会改系统的子命令都必须在动手之前调它 ——
# 早期版本 shape --rate abc 会先存快照、再让 tc 报错, 留下垃圾状态.
is_posint(){   # is_posint <值> <最小> <最大>
  case "$1" in ''|*[!0-9]*) return 1 ;; esac
  [ "$1" -ge "$2" ] 2>/dev/null && [ "$1" -le "$3" ] 2>/dev/null
}

# ── 参数推导 ────────────────────────────────────────────────────────────────
# BDP(字节) = 带宽(Mbps) * 1e6 / 8 * RTT(s)
calc_bdp(){ awk -v b="$1" -v r="$2" 'BEGIN{printf "%d", b*1000000/8*(r/1000)}'; }

# tcp_mem(页). 内核在 pressure 阈值就开始缩窗, max 是硬顶.
# 小内存机器上把 max 设成内存的一半是 OOM 主因 —— 这里固定按 1/8 与 1/4 推导.
calc_tcp_mem(){
  local ram_mb="$1"
  awk -v m="$ram_mb" 'BEGIN{
    pg=m*1024/4;                      # 总内存页数
    low=int(pg/16); pres=int(pg/8); max=int(pg/4);
    if(low<4096) low=4096; if(pres<8192) pres=8192; if(max<16384) max=16384;
    printf "%d %d %d", low, pres, max
  }'
}

# 缓冲区上限 = 2 × BDP + 2MiB, 但要受全局 TCP 预算约束.
# tcp_adv_win_scale=1 会为协议/应用预留 socket 空间, 刚好 2×BDP 没有余量.
# 300M/168ms 真机平衡换序 A/B 中, 11.25MB 平均接收 257.3M, 加 2MiB 后为
# 272.7M; 原机 13.575MB 同为 272.7M, 全部 0 重传. 固定余量能拿回主要差距,
# 又不会按比例放大 500M 以上机器的缓冲区. 500M/156ms A/B 中
# 18.75-30MB 没有可测收益.
#
# 原先是死写的 [4MB, 64MB]. 64MB 这个数在两头都错：
#   高带宽机被无谓截断 —— 2G/149ms 的机器 2×BDP 是 71MB, 被砍成 64MB,
#   接收窗口只剩 32MB, 单流上限 1.93Gbps, 刚好够不到 2G.
#   小内存机又太松 —— 1GB 的机器也允许单个 socket 占 64MB, 几条大流就吃光 tcp_mem.
#
# 改成跟 tcp_mem 挂钩：单个 socket 最多占全局 TCP 预算的 1/8, 即至少要能容下
# 8 条大流同时跑满. tcp_mem 上限本身是内存的 1/4, 所以这个值 ≈ 内存的 1/32.
# 绝对上限 256MB —— 再大就是单条连接垄断全局预算了, 收益也早已递减.
#
# 注意 rmem_max/wmem_max 是「天花板」不是预分配：开着 tcp_moderate_rcvbuf,
# 连接从 default 值起步, 只有真跑得快才长上去. 而 tcp_mem 是内核硬性拦截的总量,
# 所以调大这里不会把机器 OOM 掉, 最坏是 TCP 进入内存压力后缓冲区被自动缩小.
calc_buf_max(){   # calc_buf_max <BDP字节> <内存MB>
  awk -v b="$1" -v m="$2" 'BEGIN{
    v   = b*2 + 2097152
    cap = m*32768              # tcp_mem上限(内存1/4)的 1/8 = 内存/32, 单位字节
    if(cap > 268435456) cap = 268435456      # 绝对上限 256MB
    if(v > cap) v = cap
    if(v < 4194304) v = 4194304              # 下限 4MB, 低于此连百兆都跑不满
    printf "%d", v
  }'
}

# buf_max 是被哪个条件定住的 —— 输出里说明白, 否则用户看到一个被截断的值
# 却以为是 2×BDP, 会去怀疑别的地方（我自己就在 9300 那台上绕过弯路）.
buf_max_reason(){   # buf_max_reason <BDP字节> <内存MB> <算出的buf_max>
  awk -v b="$1" -v m="$2" -v v="$3" 'BEGIN{
    target = b*2 + 2097152
    cap = m*32768; if(cap > 268435456) cap = 268435456
    if(v <= 4194304 && target < 4194304) { print "floor 4MB"; exit }
    if(v >= cap && target > cap)         { printf "capped by tcp_mem budget"; exit }
    print "2 x BDP + 2MB headroom"
  }'
}

# 整形安全余量：按标称带宽分 5 档给固定值.
# 不用百分比是因为百分比在两端都别扭 —— 100M 机器 3% 才 3Mbit 太小,
# 2G 机器 3% 就是 60Mbit 太浪费. 分档更贴合实际.
# 余量的意义：sweep 是在某个时刻测的, 晚高峰线路会变差, 留一点缓冲避免那时暴丢包.
# 安全余量. 现役档位换算成比例大约都是 2-5%, 所以小带宽也按这个比例给,
# 不能沿用"≤100M 一律 5"—— 15 Mbps 的线上 5 就是 33% 的容量, 实测干净区
# 上限 15 时会被整形到 10, 白丢三分之一.
calc_margin(){
  local bw="$1"
  if   [ "$bw" -le 30 ]   2>/dev/null; then echo 1      # ≤30M    5 就是三分之一, 只能给 1
  elif [ "$bw" -le 60 ]   2>/dev/null; then echo 2      # 31-60M
  elif [ "$bw" -le 100 ]  2>/dev/null; then echo 5      # 61-100M  原档位
  elif [ "$bw" -le 300 ]  2>/dev/null; then echo 10     # 101-300M
  elif [ "$bw" -le 600 ]  2>/dev/null; then echo 15     # 301-600M  最常见档位
  elif [ "$bw" -le 1000 ] 2>/dev/null; then echo 25     # 601-1000M
  else                                        echo 40   # >1G      大带宽波动也大
  fi
}

# HTB 令牌桶按 4ms 的线速数据量计算, 小带宽保留原来的 32k 下限.
# 固定 32k 在 2G/7G 下仍能工作, 但四轮隔离测试显示 2G 接收吞吐低约 0.24%.
# 只放大 burst 就能拿回这部分; 去掉 quantum / fq 队列参数没有额外收益,
# 还会让高带宽 HTB 报 quantum 过大的警告, 所以其余参数保持不变.
calc_burst(){   # calc_burst <rate_mbit> -> bytes
  awk -v r="$1" 'BEGIN{v=r*500; if(v<32768)v=32768; printf "%d",v}'
}

# 预估整个调优流程会跑掉多少流量. sweep 是大头 ——
# 档数随带宽线性增长, 每档还要按该速率跑满 12 秒, 千兆机器能跑掉几十 GB.
# 有流量配额的用户必须提前知道.
# 粗扫步长随带宽放大. 固定 20 时 2Gbps 机器要扫 40 档、跑掉 137GB ——
# 精度靠后面的细扫补, 粗扫没必要那么密.
calc_step(){ awk -v b="$1" 'BEGIN{s=int(b/30/10+0.5)*10; if(s<20)s=20; printf "%d", s}'; }

# 预估流量超过这个数(GB)时, 扫描前要用户再点一次头（默认否, 无终端按否）.
# 50 大致对应 2G 带宽: 千兆机器一轮约 29 GB 不打扰, 2.5G 约 69 GB、10G 两三百 GB 必须问.
TRAFFIC_CONFIRM_GB=50

estimate_traffic_gb(){
  local st; st=$(calc_step "$1")
  # LC_ALL=C: Debian/Ubuntu 默认的 mawk 按 locale 输出小数点, de_DE / ru_RU 下会打成 "29,0",
  # 而这个数还要当参数传给 cmd_sweep 去比较(复审在容器里实测: 向导因此在扫描前退出).
  LC_ALL=C awk -v b="$1" -v st="$st" 'BEGIN{
    steps = int(b*0.4/st) + 1            # 粗扫档数 = (1.2b-0.8b)/步长
    mb  = b*10/8                         # probe   4 流 10 秒
    mb += b*0.4                          # 路径验证 40% 速率 8 秒
    mb += (steps+3) * b*12/8             # 粗扫 + 细扫 3 档, 每档 12 秒
    mb += b*10/8*2                       # verify 单流 + 4 流各 10 秒
    printf "%.1f", mb/1024
  }'
}

# 缓冲区默认值（起点）决定爬坡快慢, 但每 socket 都吃这么多额度.
#   proxy 角色并发上百条连接 → 保守, 1MB
#   bulk  角色只有少数大流   → 激进, 可到 BDP
calc_buf_default(){
  local role="$1" bdp="$2"
  case "$role" in
    proxy) echo 1048576 ;;
    bulk)  awk -v b="$bdp" 'BEGIN{v=b; if(v<1048576)v=1048576; if(v>8388608)v=8388608; printf "%d", v}' ;;
    *)     echo 2097152 ;;
  esac
}

# 调优会动到的全部内核参数. 快照和回滚都以这份清单为准 ——
# 早期版本快照只记了 14 项而 tune 设了 31 项, 回滚后有 17 项在重启前仍是调优值.
# 加参数时必须同时加到这里, 否则那个参数就回滚不掉.
# 不再设置的参数也别删: 0.5.9 起不设 netdev_budget_usecs, 但旧版本把它改成过 4000, 回滚要靠清单还原.
TUNED_KEYS="
  net.core.default_qdisc
  net.ipv4.tcp_congestion_control
  net.core.rmem_max
  net.core.wmem_max
  net.core.rmem_default
  net.core.wmem_default
  net.ipv4.tcp_rmem
  net.ipv4.tcp_wmem
  net.ipv4.tcp_mem
  net.ipv4.tcp_window_scaling
  net.ipv4.tcp_moderate_rcvbuf
  net.ipv4.tcp_adv_win_scale
  net.core.netdev_max_backlog
  net.core.netdev_budget
  net.core.netdev_budget_usecs
  net.core.optmem_max
  net.core.somaxconn
  net.ipv4.tcp_max_syn_backlog
  net.ipv4.tcp_slow_start_after_idle
  net.ipv4.tcp_no_metrics_save
  net.ipv4.tcp_mtu_probing
  net.ipv4.tcp_sack
  net.ipv4.tcp_dsack
  net.ipv4.tcp_timestamps
  net.ipv4.tcp_fastopen
  net.ipv4.tcp_syncookies
  net.ipv4.tcp_tw_reuse
  net.ipv4.tcp_fin_timeout
  net.ipv4.tcp_keepalive_time
  net.ipv4.ip_local_port_range
  vm.min_free_kbytes
  fs.file-max
  vm.swappiness
"

# ── 快照与回滚 ──────────────────────────────────────────────────────────────
# ── 包管理器锁诊断 ──────────────────────────────────────────────────────────
# 装 iperf3 失败最常见的原因不是"没网"或"包不存在", 而是 dpkg 锁被占:
# Ubuntu/Debian 首次开机 unattended-upgrades 会跑 5~30 分钟, 期间任何 apt 都装不上.
# 实测现场: 一台新开的机器 unattended-upgrades 占锁 20+ 分钟, tcpfit 静默降级成
# "只能基础调优", 用户以为是工具坏了.
apt_lock_holder(){   # 有锁则输出 "PID 进程名 已运行时长", 否则输出空
  local f pids p cmd age
  command -v fuser >/dev/null 2>&1 || return 0
  for f in /var/lib/dpkg/lock-frontend /var/lib/dpkg/lock \
           /var/cache/apt/archives/lock /var/lib/apt/lists/lock; do
    [ -e "$f" ] || continue
    pids=$(fuser "$f" 2>/dev/null | tr -s ' ' '\n' | grep -x '[0-9]*')
    for p in $pids; do
      [ "$p" = "$$" ] && continue
      cmd=$(ps -o comm= -p "$p" 2>/dev/null)
      age=$(ps -o etime= -p "$p" 2>/dev/null | tr -d ' ')
      [ -n "$cmd" ] && { printf '%s %s %s' "$p" "$cmd" "${age:-?}"; return 0; }
    done
  done
  return 0
}

# 装包失败时讲清楚到底怎么回事, 而不是只说"没有 iperf3"
explain_pkg_failure(){   # explain_pkg_failure [包名]
  local pkg="${1:-iperf3}" holder up_s up_min
  holder=$(apt_lock_holder)
  up_s=$(awk '{printf "%d", $1}' /proc/uptime 2>/dev/null || echo 99999)
  up_min=$(( up_s / 60 ))

  echo >&2
  warn "${pkg} 没装上. 原因："
  if [ -n "$holder" ]; then
    set -- $holder
    warn "  包管理器正被占用 —— PID $1（$2）已运行 $3"
    case "$2" in
      unattended-upg*|apt-get|apt|aptd|dpkg|packagekitd)
        warn "  这是系统在自动装更新, 不是故障. 它跑完就能装了."
        ;;
      *) warn "  有另一个程序正在装东西, 等它结束." ;;
    esac
    echo >&2
    echo "    等它结束（通常 5~30 分钟，新机器可能更久）：" >&2
    echo "      while fuser /var/lib/dpkg/lock-frontend >/dev/null 2>&1; do sleep 10; done; echo 可以了" >&2
    echo >&2
    echo "    想看它在干什么：" >&2
    echo "      journalctl -u unattended-upgrades -f      # 自动更新的日志" >&2
    echo "      ps -fp $1" >&2
  elif [ "$up_min" -lt 15 ]; then
    warn "  这台机器刚开机 ${up_min} 分钟."
    warn "  新装的 Ubuntu/Debian 开机后会自动跑一轮系统更新, 期间装不了别的包."
    echo >&2
    echo "    等几分钟再跑一次就行. 想确认是不是它：" >&2
    echo "      systemctl is-active unattended-upgrades apt-daily.service" >&2
  elif ! command -v apt-get >/dev/null && ! command -v dnf >/dev/null && \
       ! command -v yum >/dev/null && ! command -v apk >/dev/null; then
    warn "  认不出这台机器的包管理器."
    echo "    手动装好 iperf3 再重跑." >&2
  else
    warn "  包管理器没被占用, 装包命令本身失败了."
    echo >&2
    echo "    手动跑一次看真实报错：" >&2
    if command -v apt-get >/dev/null; then echo "      apt-get update && apt-get install -y ${pkg}" >&2
    elif command -v dnf >/dev/null;    then echo "      dnf install -y ${pkg}" >&2
    elif command -v yum >/dev/null;    then echo "      yum install -y ${pkg}" >&2
    elif command -v apk >/dev/null;    then echo "      apk add ${pkg}" >&2; fi
    echo "    常见原因: 没配软件源 / DNS 不通 / 磁盘满（df -h /）" >&2
  fi
  echo >&2
}

# ── ping 变种识别 ───────────────────────────────────────────────────────────
# `command -v ping` 存在【不代表能用】. 有服务商模板装的是 GNU inetutils 的 ping,
# 它不认 -4, 而 auto_pick_peer 是 `ping $IP_FAMILY ...`, IP_FAMILY 默认 -4 ——
# 18 个候选节点全部取不到 RTT, 最后报「公共测速服务器暂时都不可用」, 在甩锅给对端.
# 一台客户机实证: 把 -4 去掉后立刻选中最近的节点, RTT 2ms.
#
# 判定方式用"直接试"而不是解析 -V —— BusyBox 连 -V 都报 invalid option.
# 只看选项是否被拒, 不看 ping 本身通不通(有的机房挡 ICMP, 那是另一回事).
ping_supports_4(){
  local out
  out=$(ping -4 -c 1 -W 1 127.0.0.1 2>&1)
  case "$out" in
    *"invalid option"*|*"unrecognized option"*|*"illegal option"*|*"unknown option"*) return 1 ;;
  esac
  return 0
}

ping_variant(){
  command -v ping >/dev/null 2>&1 || { echo none; return; }
  ping_supports_4 && { echo iputils; return; }
  case "$(ping -V 2>&1 | head -1)" in
    *"GNU inetutils"*) echo inetutils ;;
    *BusyBox*|*busybox*) echo busybox ;;
    *) echo unknown ;;
  esac
}

# 装 iputils-ping. 成功返回 0.
install_iputils(){
  local holder; holder=$(apt_lock_holder)
  if [ -n "$holder" ]; then
    set -- $holder
    warn "包管理器正被 PID $1（$2, 已运行 $3）占用, 装不了."
    return 1
  fi
  if   command -v apt-get >/dev/null; then apt-get update -qq >/dev/null 2>&1; apt-get install -y iputils-ping >/dev/null 2>&1
  elif command -v dnf     >/dev/null; then dnf install -y iputils >/dev/null 2>&1
  elif command -v yum     >/dev/null; then yum install -y iputils >/dev/null 2>&1
  elif command -v apk     >/dev/null; then apk add iputils >/dev/null 2>&1
  else return 1; fi
  ping_supports_4
}

# 向导里调用: ping 存在但不是 iputils 时, 说清楚并尝试换掉
check_ping_variant(){
  local v; v=$(ping_variant)
  case "$v" in
    iputils) return 0 ;;
    none)    return 1 ;;   # 没装 ping, 由原有分支处理
  esac
  echo
  case "$v" in
    inetutils) warn "本机的 ping 是 GNU inetutils 版, 它不认 -4 参数." ;;
    busybox)   warn "本机的 ping 是 BusyBox 版, 参数不全." ;;
    *)         warn "本机的 ping 不认 -4 参数." ;;
  esac
  warn "  后果: 自动选对端会拿不到任何节点的延迟, 最后报「公共测速服务器暂时都不可用」."
  warn "  那句话是在甩锅给对端 —— 其实是本机 ping 的问题."
  echo
  echo "  换成标准的 iputils-ping 就好. 它只替换 ping 这一个包,"
  echo "  inetutils 的其他命令（telnet 等）不受影响, ping6 也仍然在."
  echo
  if confirm "  现在换？" y; then
    if install_iputils; then
      ok "已换成 $(ping -V 2>&1 | head -1)"
      return 0
    fi
    warn "换失败了."
    explain_pkg_failure iputils-ping
    warn "手动装: apt install -y iputils-ping  /  dnf install -y iputils"
    warn "或者在选对端那一步手动填一个 iperf3 服务器地址."
  else
    warn "跳过. 自动选对端多半会失败, 到时候手动填对端地址."
  fi
  return 1
}

# ── 运行计数 ────────────────────────────────────────────────────────────────
# 纯计数: 跑一次算一次, 不生成也不发送任何机器标识, 服务端无法区分
# "一台机器跑十次" 和 "十台各跑一次". 只额外带版本号, 用来判断旧版还有多少人在用.
#
# 关掉:  TCPFIT_NO_TELEMETRY=1   或   touch /var/lib/tcpfit/no-telemetry
#
# 三条硬约束:
#   1. 后台发, 绝不阻塞任何一步 —— 统计挂了/域名没了/用户在墙内, 调优照跑
#   2. 超时 3 秒, 失败静默
#   3. 界面显示的是【上一次拿到的】缓存值, 所以永远不会为它等待
STATS_URL="https://tcpfit.spacevps.cc/ping"
STATS_CACHE="$STATE_DIR/stats.json"

telemetry_off(){
  [ -n "${TCPFIT_NO_TELEMETRY:-}" ] && return 0
  [ -f "$STATE_DIR/no-telemetry" ] && return 0
  return 1
}

# 后台打一次, 结果写进缓存供【下次】显示. 不等待, 不检查返回码.
telemetry_ping(){
  telemetry_off && return 0
  command -v curl >/dev/null 2>&1 || return 0
  mkdir -p "$STATE_DIR" 2>/dev/null || return 0
  (
    # 关掉继承来的锁 fd. 不关的话这个后台子进程会一直持着 flock,
    # 于是同一次运行里紧接着的第二次 take_lock 被【自己】挡住,
    # 报"另一个 tcpfit 正在运行 / 可能卡死" —— 而并没有第二个实例.
    # 统计请求本身不需要锁.
    exec 9>&- 2>/dev/null || true
    out=$(curl -fsS --max-time 3 "${STATS_URL}?v=${VERSION}" 2>/dev/null) || exit 0
    # 只接受长得像 {"today":N,"total":N} 的东西, 别把错误页写进缓存
    case "$out" in
      *'"today"'*'"total"'*) printf '%s' "$out" > "$STATS_CACHE" 2>/dev/null ;;
    esac
  ) >/dev/null 2>&1 &
  return 0
}

# 读缓存, 给 banner 用. 没有缓存就返回空, banner 那一行整个不显示.
telemetry_line(){
  telemetry_off && return 0
  [ -f "$STATS_CACHE" ] || return 0
  local t n
  t=$(sed -n 's/.*"today"[[:space:]]*:[[:space:]]*\([0-9]*\).*/\1/p' "$STATS_CACHE" 2>/dev/null)
  n=$(sed -n 's/.*"total"[[:space:]]*:[[:space:]]*\([0-9]*\).*/\1/p' "$STATS_CACHE" 2>/dev/null)
  [ -n "$t" ] && [ -n "$n" ] && [ "$n" -gt 0 ] 2>/dev/null || return 0
  printf '今天 %s 次 · 累计 %s 次' "$t" "$n"
}

take_snapshot(){
  mkdir -p "$STATE_DIR"
  [ -f "$SNAPSHOT" ] && { info "Snapshot already exists, keeping the earliest one"; return; }
  # 机器已经被调过（手工或旧版本）却没有快照时, 当前状态不能当基线 ——
  # 那样 rollback 只会回到"调优后", 永远回不到出厂. 必须让用户先明确基线.
  if [ -f "$SYSCTL_FILE" ] || [ -f "$QDISC_SCRIPT" ]; then
    warn "检测到本机已有调优配置, 但没有出厂快照."
    warn "现在存快照会把「已调优状态」误记成基线, 导致 rollback 失效."
    warn "请先二选一："
    warn "  a) 手工写好出厂值到 $SNAPSHOT（格式见 docs）"
    warn "  b) 先 $(disp) rollback 回到出厂, 再重新 tune"
    warn "  c) 确认无需回滚能力, 则: touch $SNAPSHOT"
    die "已中止, 未做任何改动" 1
  fi
  local iface tmp want got; iface=$(detect_iface)
  # 快照是 rollback 的唯一依据, 写失败绝不能继续往下改机器.
  # 早期版本 `} > "$SNAPSHOT"` 之后直接打印 "Snapshot saved" 不做检查:
  # 只读文件系统 / 磁盘满 / inode 耗尽 时, 屏幕说存好了, 机器照改,
  # 而 rollback 已经没有依据了 —— 用户以为随时能退, 其实退不回去.
  # 做法: 同目录临时文件 -> 完整性检查 -> 原子改名.
  tmp=$(mktemp "${SNAPSHOT}.XXXXXX" 2>/dev/null) ||
    die "无法在 $STATE_DIR 创建快照临时文件（只读文件系统? 磁盘满?）, 未做任何改动" 1
  if ! {
    echo "# tcpfit pre-tune snapshot  $(date -u +%FT%TZ)"
    echo "KERNEL=$(uname -r)"
    for k in $TUNED_KEYS; do
      printf '%s = %s\n' "$k" "$(sysctl -n "$k" 2>/dev/null)"
    done
    echo "# route: $(ip route show default)"
    echo "# qdisc: $(tc qdisc show dev "$iface" 2>/dev/null | head -1)"
  } > "$tmp"; then
    rm -f "$tmp"; die "快照写入失败（磁盘满?）, 未做任何改动" 1
  fi
  # 完整性检查: TUNED_KEYS 每项都要落盘, 少一项回滚就少还原一项
  want=$(printf '%s\n' $TUNED_KEYS | grep -c .)
  got=$(grep -cE '^[a-z].* = ' "$tmp")
  if [ "$got" -lt "$want" ] 2>/dev/null; then
    rm -f "$tmp"; die "快照不完整（${got}/${want} 项）, 未做任何改动" 1
  fi
  mv -- "$tmp" "$SNAPSHOT" || { rm -f "$tmp"; die "快照落盘失败, 未做任何改动" 1; }
  ok "Snapshot saved: $SNAPSHOT"
  # 出厂状态同时存成 0000 存档 —— 名字固定, 后面不允许改名或删除
  mkdir -p "$ARCHIVE_DIR" 2>/dev/null
  [ -n "$(archive_path 0 2>/dev/null)" ] || {
    cp -a "$SNAPSHOT" "$ARCHIVE_DIR/0000-${ARCHIVE_ZERO_NAME}.snap" 2>/dev/null &&
      info "已建立存档 0000 ${ARCHIVE_ZERO_NAME}（出厂状态, 不可改名/删除）"
  }
}

# ── 调优存档 ────────────────────────────────────────────────────────────────
# 每次调优存一份完整状态: 参数 + sysctl + 路由 initcwnd + 整形值 + 拐点结果.
# 0000 是出厂状态, 名字固定、不可改名、不可删除、永远排最前 —— 它是最后的退路,
# 一旦被改写或删掉, 机器就再也回不到没被动过的样子.
ARCHIVE_DIR="$STATE_DIR/archives"
ARCHIVE_ZERO_NAME="出厂状态"

# 名字里不能有 / 和换行(会破坏文件名和逐行解析), 长度截到 40 个字符.
# 字节数转人话. 212992 显示成 0MB 会让人以为没设置, 所以小于 1MB 时用 KB.
# 2026-09-04T01:25:42Z -> "2026-09-04 01:25". 老快照是裸日期, 原样返回.
fmt_created(){
  local c="${1:-}" t
  case "$c" in
    *T*Z) t="${c#*T}"; printf '%s %s' "${c%%T*}" "$(printf '%s' "$t" | cut -c1-5)" ;;
    "")   printf '?' ;;
    *)    printf '%s' "$c" ;;
  esac
}

human_bytes(){
  local b="${1:-0}"
  if   [ "$b" -ge 1048576 ] 2>/dev/null; then printf '%d MB' $(( b / 1048576 ))
  elif [ "$b" -ge 1024 ]    2>/dev/null; then printf '%d KB' $(( b / 1024 ))
  else printf '%s B' "$b"; fi
}

archive_sanitize(){ printf '%s' "$1" | tr -d '\n\r/' | cut -c1-40; }

archive_path(){   # 按序号取存档路径
  local seq="$1"
  case "$seq" in ''|*[!0-9]*) return 1 ;; esac
  # 用字符串去掉前导零再补齐，避免八进制和超长数字的整数溢出。
  while [ "${#seq}" -gt 1 ] && [ "${seq#0}" != "$seq" ]; do seq=${seq#0}; done
  seq=$(printf '%4s' "$seq"); seq=${seq// /0}
  find "$ARCHIVE_DIR" -maxdepth 1 -name "${seq}-*.snap" 2>/dev/null | head -1
}
archive_seq_of(){ local b; b=$(basename "$1"); printf '%s' "${b%%-*}"; }
archive_name_of(){ local b; b=$(basename "$1" .snap); printf '%s' "${b#*-}"; }

archive_next_seq(){
  local last=-1 f s
  for f in "$ARCHIVE_DIR"/*.snap; do
    [ -e "$f" ] || continue
    s=$(archive_seq_of "$f"); s=$((10#$s))
    [ "$s" -gt "$last" ] && last=$s
  done
  printf '%04d' $(( last + 1 ))
}

# 老版本只有 pre-tune.snapshot. 有它没有 0000 时, 原样搬成 0000 ——
# 它就是那台机器的出厂状态, 不能丢.
archive_migrate(){
  mkdir -p "$ARCHIVE_DIR" 2>/dev/null || return 0
  [ -n "$(archive_path 0)" ] && return 0
  [ -f "$SNAPSHOT" ] || return 0
  cp -a "$SNAPSHOT" "$ARCHIVE_DIR/0000-${ARCHIVE_ZERO_NAME}.snap" 2>/dev/null &&
    info "已把旧快照收进存档 0000 ${ARCHIVE_ZERO_NAME}"
}

# archive_write <序号> <名字>  —— 把当前状态写成存档
archive_write(){
  local seq="$1" name="$2" iface rate f tmp
  mkdir -p "$ARCHIVE_DIR" || return 1
  iface=$(detect_iface)
  rate=$(tc_rate_mbit "$(tc class show dev "$iface" 2>/dev/null)")
  f="$ARCHIVE_DIR/${seq}-${name}.snap"
  tmp=$(mktemp "$ARCHIVE_DIR/.archive.XXXXXX") || return 1
  {
    echo "# tcpfit archive"
    echo "SEQ=$seq"
    echo "NAME=$name"
    echo "CREATED=$(date -u +%FT%TZ)"
    echo "CREATED_LOCAL=$(date +'%F %T %Z')"
    echo "TZ_NAME=$(timedatectl show -p Timezone --value 2>/dev/null || cat /etc/timezone 2>/dev/null || echo '?')"
    echo "TCPFIT_VERSION=$VERSION"
    echo "KERNEL=$(uname -r)"
    [ -n "${ARCH_ROLE:-}" ] && echo "PARAM_ROLE=$ARCH_ROLE"
    [ -n "${ARCH_BW:-}"   ] && echo "PARAM_BW=$ARCH_BW"
    [ -n "${ARCH_RTT:-}"  ] && echo "PARAM_RTT=$ARCH_RTT"
    [ -n "${ARCH_PEER:-}" ] && echo "PARAM_PEER=$ARCH_PEER"
    echo "SHAPE_RATE=${rate:-none}"
    [ "${ARCH_INCLUDE_SWEEP:-1}" = 1 ] && [ -f "$STATE_DIR/sweep.result" ] &&
      sed 's/^/SWEEP_/' "$STATE_DIR/sweep.result"
    for k in $TUNED_KEYS; do
      printf '%s = %s\n' "$k" "$(sysctl -n "$k" 2>/dev/null)"
    done
    echo "# route: $(ip route show default)"
    echo "# qdisc: $(tc qdisc show dev "$iface" 2>/dev/null | head -1)"
  } > "$tmp" || { rm -f "$tmp"; return 1; }
  mv -- "$tmp" "$f" || { rm -f "$tmp"; return 1; }
  printf '%s' "$f"
}

# archive_save [名字]  —— 存一份新的. 名字空则用日期时间.
archive_save(){
  archive_migrate
  local name seq f
  name=$(archive_sanitize "${1:-}")
  [ -n "$name" ] || name="$(date +%m%d-%H%M)"
  seq=$(archive_next_seq)
  # 0000 只能由 take_snapshot 在出厂状态下创建, 不接受用户占位
  [ "$seq" = 0000 ] && { warn "还没有出厂快照, 先跑一次 tune 或 rollback"; return 1; }
  f=$(archive_write "$seq" "$name") || { warn "存档写入失败: $name"; return 1; }
  ok "已存档 $seq $name"
  info "  $f"
}

archive_list(){
  archive_migrate
  local f seq name created ver rate n=0 cur_cc cur_rate mark marked=0
  [ -d "$ARCHIVE_DIR" ] || { warn "还没有任何存档"; return 0; }
  # 判断"当前处于哪个存档": 拿现在的拥塞控制+整形值去比对
  cur_cc=$(sysctl -n net.ipv4.tcp_congestion_control 2>/dev/null)
  cur_rate=$(tc_rate_mbit "$(tc class show dev "$(detect_iface)" 2>/dev/null)")
  local cur_rmem; cur_rmem=$(sysctl -n net.core.rmem_max 2>/dev/null)
  echo
  printf '  %s %s %s %s %s\n' "$(_pad 序号 6)" "$(_pad 名字 20)" "$(_pad '时间 (UTC)' 18)" "$(_pad 整形 8)" "版本"
  printf '  %s\n' "$(printf '─%.0s' $(seq 1 68))"
  while IFS= read -r f; do
    seq=$(archive_seq_of "$f"); name=$(archive_name_of "$f")
    created=$(awk -F= '/^CREATED=/{print $2; exit}' "$f")
    [ -n "$created" ] || created=$(awk '/^# tcpfit pre-tune snapshot/{print $NF; exit}' "$f")
    ver=$(awk -F= '/^TCPFIT_VERSION=/{print $2; exit}' "$f")
    rate=$(awk -F= '/^SHAPE_RATE=/{print $2; exit}' "$f")
    case "$rate" in none|"") rate="-" ;; *) rate="${rate}M" ;; esac
    # 当前状态标记: 整形值对得上就算(拥塞控制太粗, 多个存档可能都是 bbr)
    # 判"当前"要三个都对上: 整形值 + 拥塞控制 + 缓冲区上限.
    # 只看整形值不够 —— 出厂和调优后都可能是"无整形", 会标错到出厂那行.
    mark=""
    if [ "$marked" = 0 ]; then
      local a_cc a_rmem
      a_cc=$(awk -F'[ =]+' '/^net\.ipv4\.tcp_congestion_control/{print $2; exit}' "$f")
      a_rmem=$(awk -F'[ =]+' '/^net\.core\.rmem_max/{print $2; exit}' "$f")
      if { { [ "$rate" = "-" ] && [ -z "$cur_rate" ]; } || [ "$rate" = "${cur_rate}M" ]; } &&
         [ "$a_cc" = "$cur_cc" ] && [ "$a_rmem" = "$cur_rmem" ]; then
        mark=" ${green}<= 当前${plain}"; marked=1
      fi
    fi
    n=$((n+1))
    printf '  %s %s %s %s %s%s%s\n' \
      "$(_pad "$seq" 6)" "$(_pad "$name" 20)" "$(_pad "$(fmt_created "$created")" 18)" \
      "$(_pad "$rate" 8)" "${ver:--}" \
      "$([ "$seq" = 0000 ] && printf ' %s' "$(_c '0;33' '(出厂, 不可改名/删除)')")" "$mark"
  done < <(find "$ARCHIVE_DIR" -maxdepth 1 -name '*.snap' 2>/dev/null | sort)
  [ "$n" = 0 ] && warn "还没有任何存档"
  echo
  return 0
}

# 找存档: 接受序号(0/0000/1) 或名字
# 查找顺序: 纯数字当序号, 否则当名字.
# 所以名字取成纯数字(比如 "123")时会被当序号找, 找不到才回退 —— 别那样命名.
# 按名字找时要处理重名: 自动名是 base-<带宽>-rtt<RTT>, 同一分钟跑两次 tune
# 就会撞名. 早期实现返回第一个匹配(序号最小的那个), 于是"恢复我那个
# base-220M-rtt150"会静默恢复成较旧的一份 —— 配置不一样, 用户看不出来.
# 现在重名就列出候选并要求用序号, 宁可多问一句.
archive_find_by_name(){
  local want="$1" f hits=0 last=""
  for f in "$ARCHIVE_DIR"/*.snap; do
    [ -e "$f" ] || continue
    [ "$(archive_name_of "$f")" = "$want" ] || continue
    hits=$(( hits + 1 )); last="$f"
  done
  [ "$hits" = 0 ] && return 1
  if [ "$hits" -gt 1 ]; then
    warn "有 ${hits} 个存档都叫 ${want}, 请改用序号:" >&2
    for f in "$ARCHIVE_DIR"/*.snap; do
      [ -e "$f" ] || continue
      [ "$(archive_name_of "$f")" = "$want" ] || continue
      echo "    $(archive_seq_of "$f")   $want" >&2
    done
    return 2          # 2 = 重名, 提示已经打过, 调用方不要再说"找不到"
  fi
  printf '%s' "$last"; return 0
}

archive_find(){
  local want="$1" f
  case "$want" in
    ''|*[!0-9]*) archive_find_by_name "$want" ;;
    *)           f=$(archive_path "$want"); [ -n "$f" ] && { printf '%s' "$f"; return 0; }
                 # 序号没命中, 再按名字找一遍(有人可能真把存档叫 "123")
                 archive_find_by_name "$want" ;;
  esac
}

# 恢复存档里的路由窗口. 只把【窗口值】套到当前路由上, 不回放存档里的整条路由 ——
# 存档之后网关 / metric / 网段都可能变了(换 IP、迁移、重配网络):
#   网关变了 → 默认路由被指回旧网关, 真机上直接断网
#   metric 变了 → metric 是路由的键, replace 会新建一条, 留下两条默认路由
#   网段变了 → 内核拒绝, 而早期版本在那之前已经把持久化入口全删了
# 和 cmd_rollback 同一套做法(route_restore_windows); 只有当前完全没有默认路由时
# 才把存档那条整条装回去. 运行时改成功之后再换持久化入口.
archive_restore_route(){
  local route="$1" token skip=0 rif
  local -a args=() windows=()
  INITCWND_DROPIN_REMOVED=0
  if [ -z "$route" ]; then
    remove_initcwnd_persistence || return 1
    rm -f "$PPP_HOOK" "$INITCWND_MARKER" "$INITCWND_VALS" || return 1
    [ "$INITCWND_DROPIN_REMOVED" = 0 ] || networkd_pending_note
    return 0
  fi
  read -r -a args <<< "$route"
  for token in "${args[@]}"; do
    if [ "$skip" = 1 ]; then
      case "$token" in ''|*[!0-9]*) warn "存档中的路由窗口无效"; return 1 ;; esac
      windows+=("$token"); skip=0
    else
      case "$token" in initcwnd|initrwnd) windows+=("$token"); skip=1 ;; esac
    fi
  done
  [ "$skip" = 0 ] || return 1
  rif=$(route_field dev "$route")
  if ! route_restore_windows "$rif" "${windows[@]+"${windows[@]}"}"; then
    if [ -z "$(ip -4 route show default 2>/dev/null)" ]; then
      ip -4 route replace "${args[@]}" 2>/dev/null || { warn "默认路由还原失败"; return 1; }
    else
      warn "默认路由窗口还原失败, 开机持久化保持不变"; return 1
    fi
  fi
  remove_initcwnd_persistence || return 1
  rm -f "$PPP_HOOK" "$INITCWND_MARKER" "$INITCWND_VALS" || return 1
  if [ "${#windows[@]}" = 0 ]; then
    ok "默认路由窗口已还原（存档里没有窗口设置）"
    [ "$INITCWND_DROPIN_REMOVED" = 0 ] || networkd_pending_note
    return 0
  fi
  # 持久化和 cmd_tune 走同一个函数. 早期这里自己挑路, 跟 tune 分歧过两次:
  # 只认 networkd-dispatcher 时 PPPoE 机器恢复必失败; 只认前两条时普通 Debian VPS
  # (tune 能用单元持久化的那种) 恢复存档反而报"无法持久化".
  if persist_initcwnd "$rif" "${windows[@]}"; then
    mkdir -p "$STATE_DIR" && : > "$INITCWND_MARKER" || return 1
    printf '%s\n' "${windows[*]}" > "$INITCWND_VALS"
    ok "默认路由窗口已还原为 ${windows[*]}, 开机持久化交给 ${INITCWND_VIA}"
    case "$INITCWND_VIA" in
      systemd-networkd*) networkd_pending_note ;;
      *) [ "$INITCWND_DROPIN_REMOVED" = 0 ] || networkd_pending_note ;;
    esac
    return 0
  fi
  warn "路由窗口已即时还原, 但本机没有可用的持久化入口（非 PPP、没有 systemd-networkd、无法启用 systemd 单元）, 窗口值无法持久化"
  return 1
}

archive_restore(){
  archive_migrate
  local f="$1" iface rate route was_cc was_rmem was_rate now_cc now_rmem now_rate
  local tmp k v failed=0
  [ -r "$f" ] && [ -f "$f" ] || { warn "无法读取存档: $f"; return 1; }
  # 出厂回滚必须移除持久化入口，与 rollback 使用同一路径。
  if [ "$(archive_seq_of "$f")" = 0000 ]; then
    local SNAPSHOT="$f"
    cmd_rollback
    return $?
  fi
  grep -qE '^(net|vm|fs)\.[^=]+=' "$f" || { warn "存档没有 sysctl 数据"; return 1; }
  rate=$(awk -F= '/^SHAPE_RATE=/{print $2; exit}' "$f")
  if [ -n "$rate" ] && [ "$rate" != none ]; then
    is_posint "$rate" 1 100000 || { warn "存档中的整形值无效: $rate"; return 1; }
  fi
  iface=$(detect_iface)
  [ -n "$iface" ] || { warn "找不到默认路由网卡"; return 1; }
  tmp=$(mktemp "${SYSCTL_FILE}.restore.XXXXXX") || return 1
  was_cc=$(sysctl -n net.ipv4.tcp_congestion_control 2>/dev/null)
  was_rmem=$(sysctl -n net.core.rmem_max 2>/dev/null)
  was_rate=$(tc_rate_mbit "$(tc class show dev "$iface" 2>/dev/null)")
  info "回滚到存档 $(archive_seq_of "$f") $(archive_name_of "$f")…"
  # 同步启动配置；本内核拒绝的值不留成启动时必然失败的配置项。
  printf '# tcpfit restored archive %s\n' "$(archive_seq_of "$f")" > "$tmp"
  if grep -qE '^net\.ipv4\.tcp_congestion_control[[:space:]]*=[[:space:]]*bbr[[:space:]]*$' "$f"; then
    modprobe tcp_bbr 2>/dev/null || true
  fi
  while IFS='=' read -r k v; do
    k=$(echo "$k" | xargs); v=$(echo "$v" | xargs)
    [ -n "$k" ] && [ -n "$v" ] || continue
    if sysctl -qw "$k=$v" 2>/dev/null; then
      printf '%s = %s\n' "$k" "$v" >> "$tmp" || failed=1
    else
      warn "参数还原失败: $k"
      printf '# 本内核拒绝: %s = %s\n' "$k" "$v" >> "$tmp"
      failed=1
    fi
  done < <(grep -E '^(net|vm|fs)\.' "$f")
  chmod 644 "$tmp" && mv -- "$tmp" "$SYSCTL_FILE" || {
    rm -f "$tmp"; warn "sysctl 启动配置写入失败，运行值可能已改变"; return 1;
  }
  [ "$failed" = 0 ] && ok "sysctl 已按存档还原并持久化"
  if [ "$(sysctl -n net.ipv4.tcp_congestion_control 2>/dev/null)" = bbr ]; then
    echo tcp_bbr > "$BBR_MODULE_FILE" || failed=1
  else
    rm -f "$BBR_MODULE_FILE" || failed=1
  fi
  # 路由(initcwnd)
  route=$(awk '/^# route: /{sub(/^# route: /, ""); print; exit}' "$f")
  archive_restore_route "$route" || failed=1
  # 整形
  if [ -n "$rate" ] && [ "$rate" != none ]; then
    if cmd_shape --rate "$rate" >/dev/null 2>&1 &&
       systemctl is-enabled tcpfit-qdisc.service >/dev/null 2>&1; then
      ok "整形已还原为 ${rate} Mbit，开机服务已启用"
    else warn "整形还原失败"; failed=1; fi
  else
    local shape_failed=0 default_kind
    if ! systemctl disable --now tcpfit-qdisc.service >/dev/null 2>&1; then
      # 未安装服务的干净机器无需停用；已有服务失败则必须报告。
      if [ -f "$QDISC_UNIT" ] || [ -f "$QDISC_SCRIPT" ]; then shape_failed=1; fi
    fi
    rm -f "$QDISC_UNIT" "$QDISC_SCRIPT" || shape_failed=1
    systemctl daemon-reload >/dev/null 2>&1 || shape_failed=1
    # sysctl 已还原。删除整形后让内核补回其默认队列，不强制改成 fq。
    if ! qdisc_remove_root "$iface"; then
      default_kind=$(sysctl -n net.core.default_qdisc 2>/dev/null)
      [ -n "$default_kind" ] && [ "$(qdisc_root_kind "$iface")" = "$default_kind" ] || shape_failed=1
    fi
    if [ "$shape_failed" = 0 ]; then ok "整形已移除，开机整形服务已停用"
    else warn "整形移除或服务停用失败"; failed=1; fi
  fi
  now_cc=$(sysctl -n net.ipv4.tcp_congestion_control 2>/dev/null)
  now_rmem=$(sysctl -n net.core.rmem_max 2>/dev/null)
  echo
  printf '  %s %s   %s\n' "$(_pad '' 12)" "$(_pad 回滚前 14)" "回滚后"
  printf '  %s %s   %s\n' "$(_pad 拥塞控制 12)" "$(_pad "$was_cc" 14)" "$now_cc"
  printf '  %s %s   %s\n' "$(_pad 缓冲区上限 12)" "$(_pad "$(human_bytes "$was_rmem")" 14)" "$(human_bytes "$now_rmem")"
  now_rate=$(tc_rate_mbit "$(tc class show dev "$iface" 2>/dev/null)")
  printf '  %s %s   %s\n' "$(_pad 整形 12)" "$(_pad "${was_rate:-无}${was_rate:+M}" 14)" "${now_rate:-无}${now_rate:+M}"
  echo
  [ "$failed" = 0 ] || warn "存档仅部分还原，请检查以上失败项"
  return "$failed"
}

archive_rename(){
  local f="$1" new; new=$(archive_sanitize "$2")
  [ "$(archive_seq_of "$f")" = 0000 ] && { warn "0000 ${ARCHIVE_ZERO_NAME} 是出厂状态, 不能改名"; return 1; }
  [ -n "$new" ] || { warn "新名字不能为空"; return 1; }
  mv "$f" "$(dirname "$f")/$(archive_seq_of "$f")-${new}.snap" && ok "已改名为 $new"
}

archive_delete(){
  local f="$1"
  [ "$(archive_seq_of "$f")" = 0000 ] && { warn "0000 ${ARCHIVE_ZERO_NAME} 是最后的退路, 不能删除"; return 1; }
  rm -f "$f" && ok "已删除存档 $(archive_seq_of "$f")"
}

cmd_archive(){
  need_root
  take_lock
  migrate_legacy
  archive_migrate
  local sub="${1:-list}"; shift 2>/dev/null || true
  case "$sub" in
    list|ls|"")  archive_list ;;
    save)        take_lock; archive_save "${1:-}" ;;
    restore)     [ -n "${1:-}" ] || die "用法: $(disp) archive restore <序号|名字>"
                 take_lock
                 local f rc=0; f=$(archive_find "$1") || rc=$?
                 [ "$rc" = 2 ] && return 1                       # 重名, 上面已列出候选
                 [ -n "$f" ] || die "找不到存档: $1"
                 archive_restore "$f" ;;
    rename)      [ -n "${2:-}" ] || die "用法: $(disp) archive rename <序号> <新名字>"
                 local f rc=0; f=$(archive_find "$1") || rc=$?
                 [ "$rc" = 2 ] && return 1                       # 重名, 上面已列出候选
                 [ -n "$f" ] || die "找不到存档: $1"
                 archive_rename "$f" "$2" ;;
    delete|rm)   [ -n "${1:-}" ] || die "用法: $(disp) archive delete <序号>"
                 local f rc=0; f=$(archive_find "$1") || rc=$?
                 [ "$rc" = 2 ] && return 1                       # 重名, 上面已列出候选
                 [ -n "$f" ] || die "找不到存档: $1"
                 archive_delete "$f" ;;
    show|cat)    [ -n "${1:-}" ] || die "用法: $(disp) archive show <序号>"
                 local f rc=0; f=$(archive_find "$1") || rc=$?
                 [ "$rc" = 2 ] && return 1                       # 重名, 上面已列出候选
                 [ -n "$f" ] || die "找不到存档: $1"
                 cat "$f" ;;
    *)           die "未知子命令: $sub（list / save / restore / rename / delete / show）" ;;
  esac
}

cmd_uninstall(){
  need_root
  take_lock
  migrate_legacy
  local keep_archives=0 yes=0
  while [ $# -gt 0 ]; do
    case "$1" in
      --keep-archives) keep_archives=1; shift ;;
      --yes|-y) yes=1; shift ;;
      *) die "未知参数: $1（--keep-archives / --yes）" ;;
    esac
  done

  echo
  warn "卸载会做这些事："
  echo "    1. 回滚到出厂状态（sysctl / initcwnd / 整形全部还原）"
  echo "    2. 删掉 $SYSCTL_FILE"
  echo "    3. 停用并删掉 tcpfit-qdisc 服务和脚本"
  echo "    4. 撤掉 initcwnd 的开机持久化（钩子 / 单元 / networkd 配置）"
  if [ "$keep_archives" = 1 ]; then
    echo "    5. 保留 $STATE_DIR（存档和快照）"
  else
    echo "    5. 删掉 $STATE_DIR（存档和快照, 之后无法再回滚）"
  fi
  echo "    6. 删掉 $SELF_PATH 本身"
  echo
  echo "  不会碰: swap（要删自己 swapoff）、iperf3、ping 等装过的包."
  echo
  [ "$yes" = 1 ] || confirm "  确定卸载？" n || { info "已取消, 什么都没动"; return 0; }

  # 这里不要再 info "回滚中…" —— cmd_rollback 自己会打, 否则屏幕上出现两遍.
  cmd_rollback || { warn "回滚未完全成功，已停止卸载并保留存档，请处理失败项后重试"; return 1; }

  # 配置和服务已由 cmd_rollback 清理并检查，不再重复执行。
  ok "配置和服务已移除"

  # 保留当前脚本到最后；旧入口清理失败时也保留存档，便于重试。
  rm -f "$LEGACY_SELF" 2>/dev/null || { warn "旧入口删除失败: $LEGACY_SELF，已停止卸载"; return 1; }
  if [ "$keep_archives" = 1 ]; then
    info "存档保留在 $STATE_DIR"
  else
    rm -rf "$STATE_DIR" || { warn "存档目录未完全删除: $STATE_DIR，已保留当前脚本供重试"; return 1; }
    ok "存档和快照已删除"
  fi

  # 最后删自己. 正在执行的脚本被删掉不影响当前进程(inode 还在), 但要放在最后.
  local me="$SELF_PATH"
  if [ -e "$me" ] || [ -L "$me" ]; then
    rm -f "$me" || { warn "脚本删除失败: $me，卸载未完成"; return 1; }
    ok "已删除 $me"
  fi
  echo
  ok "tcpfit 已卸载. 机器回到了出厂状态."
  echo "  重新装:  bash <(curl -fsSL $SELF_URL)"
}

cmd_rollback(){
  need_root
  take_lock
  migrate_legacy
  local purge_swap=0 failed=0 k v route
  while [ $# -gt 0 ]; do
    case "$1" in
      --purge-swap) purge_swap=1; shift ;;
      *) die "未知参数: $1" ;;
    esac
  done
  info "回滚中…"
  # 先取回滚前的值 —— 必须在删文件、改 sysctl 之前, 否则读到的就是回滚后的.
  # 恢复其他存档时有这张对照表, 唯独最常用的"回到出厂"没有, 不合理.
  local was_cc was_rmem was_rate now_cc now_rmem now_rate
  was_cc=$(sysctl -n net.ipv4.tcp_congestion_control 2>/dev/null)
  was_rmem=$(sysctl -n net.core.rmem_max 2>/dev/null)
  was_rate=$(tc_rate_mbit "$(tc class show dev "$(detect_iface)" 2>/dev/null)")
  rm -f "$SYSCTL_FILE" "$ROUTE_HOOK" "$PPP_HOOK" "$INITCWND_MARKER" "$INITCWND_VALS" "$INITCWND_SCRIPT" "$BBR_MODULE_FILE" || {
    warn "调优配置未完全删除，请检查文件权限或只读文件系统"; failed=1;
  }
  # initcwnd 的开机入口(单元 / dispatcher 钩子 / networkd drop-in)也要撤. 留着的话
  # 下次开机或 networkd 重启时窗口又被写回去, 回滚等于没回.
  INITCWND_DROPIN_REMOVED=0
  remove_initcwnd_persistence || { warn "initcwnd 开机入口未完全删除"; failed=1; }
  if [ "$INITCWND_DROPIN_REMOVED" = 1 ]; then
    info "已删除 networkd 里的窗口配置"
    networkd_pending_note
  fi
  local service_stopped=1
  if ! systemctl disable --now tcpfit-qdisc.service >/dev/null 2>&1; then
    # 没装过整形服务时 disable 也会失败；已有服务停用失败则保留文件供重试。
    if [ -e "$QDISC_UNIT" ] || [ -L "$QDISC_UNIT" ] || [ -e "$QDISC_SCRIPT" ] || [ -L "$QDISC_SCRIPT" ]; then
      warn "整形服务停用失败，已保留服务文件供重试"; failed=1; service_stopped=0
    fi
  fi
  if [ "$service_stopped" = 1 ]; then
    rm -f "$QDISC_UNIT" "$QDISC_SCRIPT" || { warn "整形服务文件未完全删除"; failed=1; }
  fi
  systemctl daemon-reload >/dev/null 2>&1 || { warn "systemd 配置重载失败"; failed=1; }
  local iface; iface=$(detect_iface)
  # 逐项写回快照值
  if [ -f "$SNAPSHOT" ]; then
    while IFS='=' read -r k v; do
      k=$(echo "$k" | xargs); v=$(echo "$v" | xargs)
      [ -n "$k" ] && [ -n "$v" ] || continue
      if ! sysctl -qw "$k=$v" 2>/dev/null; then warn "参数还原失败: $k"; failed=1; fi
    done < <(grep -E '^(net|vm|fs)\.' "$SNAPSHOT")
    [ "$failed" = 0 ] && ok "已按快照还原 sysctl"
    route=$(awk '/^# route: /{sub(/^# route: /, ""); print; exit}' "$SNAPSHOT")
    if [ -n "$route" ]; then
      # 从快照那条路由里取出【网卡】和【原有窗口值】, 套到当前路由上.
      local snap_if snap_t snap_skip=0
      local -a snap_args=() snap_win=()
      snap_if=$(route_field dev "$route")
      read -r -a snap_args <<< "$route"
      for snap_t in "${snap_args[@]}"; do
        if [ "$snap_skip" = 1 ]; then snap_win+=("$snap_t"); snap_skip=0; continue; fi
        case "$snap_t" in
          initcwnd|initrwnd) snap_win+=("$snap_t"); snap_skip=1 ;;
        esac
      done
      if route_restore_windows "$snap_if" "${snap_win[@]+"${snap_win[@]}"}"; then
        [ "${#snap_win[@]}" = 0 ] || info "已还原调优前的窗口值: ${snap_win[*]}"
      elif [ -z "$(ip -4 route show default 2>/dev/null)" ]; then
        # 压根没有默认路由了, 这时才把快照那条整体塞回去
        local -a route_args=()
        read -r -a route_args <<< "$route"
        ip -4 route replace "${route_args[@]}" 2>/dev/null ||
          { warn "默认路由还原失败"; failed=1; }
      else
        warn "默认路由窗口未完全还原"; failed=1
      fi
    fi
  else
    warn "找不到快照, 仅移除了调优文件；重启后内核默认值生效"
    failed=1
  fi
  # 先还原 default_qdisc，再移除整形；内核补回的默认队列才使用出厂值。
  if ! qdisc_remove_root "$iface"; then
    if [ "$(qdisc_root_kind "$iface")" = htb ]; then
      warn "整形移除失败"; failed=1
    fi
  fi
  # swap 默认不动 —— 删掉一个正在用的 swap 可能让机器立刻 OOM.
  # 想连 swap 一起撤销要显式加 --purge-swap.
  if [ "$purge_swap" = 1 ]; then
    if [ ! -f /swapfile ]; then
      info "没有 /swapfile, 跳过"
    elif [ ! -f "$STATE_DIR/swapfile.owned" ]; then
      warn "/swapfile 不是 tcpfit 创建的, 拒绝删除. 要删请自己确认后手动操作"
    elif ! swapoff /swapfile 2>/dev/null; then
      # swapoff 失败通常是内存不够把页换回来, 这时删文件会让内核继续写一个
      # 已删除的 inode, 空间也不会释放 —— 必须停手
      warn "swapoff /swapfile 失败（内存可能不足以换回), 未删除. 释放内存后重试"
    else
      rm -f /swapfile
      sed -i '\#^/swapfile #d' /etc/fstab
      rm -f "$STATE_DIR/swapfile.owned"
      ok "已移除 /swapfile 及其 fstab 条目"
    fi
  elif [ -f /swapfile ]; then
    info "/swapfile 保留. 要一并删除: $(disp) rollback --purge-swap"
  fi
  now_cc=$(sysctl -n net.ipv4.tcp_congestion_control 2>/dev/null)
  now_rmem=$(sysctl -n net.core.rmem_max 2>/dev/null)
  now_rate=$(tc_rate_mbit "$(tc class show dev "$iface" 2>/dev/null)")
  echo
  printf '  %s %s   %s\n' "$(_pad '' 12)" "$(_pad 回滚前 14)" "回滚后"
  printf '  %s %s   %s\n' "$(_pad 拥塞控制 12)" "$(_pad "$was_cc" 14)" "$now_cc"
  printf '  %s %s   %s\n' "$(_pad 缓冲区上限 12)" "$(_pad "$(human_bytes "$was_rmem")" 14)" "$(human_bytes "$now_rmem")"
  printf '  %s %s   %s\n' "$(_pad 整形 12)" "$(_pad "${was_rate:-无}${was_rate:+M}" 14)" "${now_rate:-无}${now_rate:+M}"
  echo
  if [ "$failed" = 0 ]; then ok "回滚完成"
  else warn "仅部分回滚成功，请检查以上失败项"; fi
  return "$failed"
}

# ── 基础调优 ────────────────────────────────────────────────────────────────
cmd_tune(){
  need_root
  take_lock
  migrate_legacy
  self_install
  local role=mixed bw="" rtt="" no_initcwnd=0 peer="" ARCH_SAVE_NAME=""
  while [ $# -gt 0 ]; do
    case "$1" in
      --role) role="$2"; shift 2 ;;
      --bw)   bw="$2";   shift 2 ;;
      --rtt)  rtt="$2";  shift 2 ;;
      --peer) peer="$2"; shift 2 ;;
      --no-initcwnd) no_initcwnd=1; shift ;;
      --save) ARCH_SAVE_NAME="$2"; shift 2 ;;
      *) die "未知参数: $1" ;;
    esac
  done
  case "$role" in proxy|bulk|mixed) ;; *) die "role 只能是 proxy / bulk / mixed" ;; esac

  # 目标必须在 detect_iface 【之前】设好. 早期版本先缓存了 iface、
  # 后面才 set_route_target, 于是多出口机器上 iface 仍是主表 default 的那块,
  # 后续 probe_bandwidth / 整形全都作用在错网卡上.
  set_route_target "$peer"
  local iface ram; iface=$(detect_iface); ram=$(detect_ram_mb)
  [ -n "$iface" ] || die "找不到默认路由网卡"
  # --rtt 给了就用给的, 没给就用固定值. 不再探测, 所以不会再出现
  # "无法确定 RTT" 这种把用户指向死路的报错（向导里根本没地方填 --rtt）.
  if [ -n "$rtt" ]; then
    is_posint "$rtt" 1 2000 || die "--rtt 必须是 1-2000 之间的整数（毫秒）"
  else
    rtt="$DEFAULT_RTT"
  fi
  # --bw auto: 现场探测. 虚拟网卡读不到标称速率, 这是最常见的情况.
  if [ "$bw" = auto ]; then
    [ -n "$peer" ] || die "--bw auto 需要同时给 --peer <近处的iperf3服务器>"
    command -v iperf3 >/dev/null || die "--bw auto 需要 iperf3"
    info "Probing available bandwidth..."
    bw=$(probe_bandwidth "$peer" "$iface") || bw=""
    [ -n "$bw" ] && ok "Measured ~${bw} Mbps" || die "bandwidth probe failed" 2
  fi
  [ -n "$bw" ] || bw=$(detect_link_mbps "$iface")
  if ! { [ -n "$bw" ] && [ "$bw" -gt 0 ] 2>/dev/null; }; then
    warn "本机是虚拟网卡, 读不到标称速率. 三选一："
    warn "  a) 知道套餐带宽:  $(disp) tune --role $role --bw <Mbps>"
    warn "  b) 现场探测:      $(disp) tune --role $role --bw auto --peer <近处iperf3服务器>"
    warn "  c) 先单独探测:    $(disp) probe --peer <近处iperf3服务器>"
    die "无法确定带宽, 已中止" 1
  fi

  take_snapshot

  local bdp buf_max buf_def tcp_mem
  bdp=$(calc_bdp "$bw" "$rtt")
  buf_max=$(calc_buf_max "$bdp" "$ram")
  buf_def=$(calc_buf_default "$role" "$bdp")
  tcp_mem=$(calc_tcp_mem "$ram")

  info "Derived from: ${bw} Mbps / RTT ${rtt} ms / ${ram} MB RAM / role $role"
  kv "  BDP"            "$(awk -v v="$bdp" 'BEGIN{printf "%.1f MB", v/1048576}')"
  kv "  Buffer max"     "$(awk -v v="$buf_max" 'BEGIN{printf "%.0f MB", v/1048576}')  ($(buf_max_reason "$bdp" "$ram" "$buf_max"))"
  kv "  Buffer default" "$(awk -v v="$buf_def" 'BEGIN{printf "%.0f MB", v/1048576}')  (role $role)"
  kv "  tcp_mem"        "$(echo "$tcp_mem" | awk '{printf "%.0fM / %.0fM / %.0fM", $1*4/1024, $2*4/1024, $3*4/1024}')  (RAM 1/16, 1/8, 1/4)"

  modprobe tcp_bbr 2>/dev/null
  echo tcp_bbr > "$BBR_MODULE_FILE"
  local cc=bbr
  has_word "$(sysctl -n net.ipv4.tcp_available_congestion_control 2>/dev/null)" bbr || {
    warn "kernel has no BBR, falling back to cubic (much smaller gain)"; cc=cubic; }

  # 写不出去就必须停下: 只改了运行时值、开机配置没落盘, 那不是"基础调优成功" ——
  # 重启全丢, 而屏幕报的是 applied.
  # 「能创建」和「非空」都不足以证明写完整: ulimit -f 限制下 cat 会报
  # File too large 但已经留下 1024 字节的截断内容, 两个检查都能通过.
  # 所以: 同目录临时文件 -> 检查 cat 的退出状态 -> 结尾哨兵行 -> 原子改名.
  # 失败时原配置保持不动.
  local _sc_tmp
  _sc_tmp=$(mktemp "${SYSCTL_FILE}.XXXXXX" 2>/dev/null) ||
    die "无法在 $(dirname "$SYSCTL_FILE") 创建临时文件（只读文件系统? 磁盘满?）, 未做任何改动" 1
  if ! cat > "$_sc_tmp" <<EOF
# 由 tcpfit v$VERSION 生成  $(date -u +%FT%TZ)
# 带宽=${bw}Mbps  RTT=${rtt}ms  内存=${ram}MB  角色=${role}
# 勿手改；要改用 tcpfit tune 重新生成

net.core.default_qdisc = fq
net.ipv4.tcp_congestion_control = $cc

# 缓冲区：上限=2×BDP+2MiB余量, 默认值按角色（默认值决定爬坡快慢, 也决定每连接内存占用）
net.core.rmem_max = $buf_max
net.core.wmem_max = $buf_max
net.core.rmem_default = $buf_def
net.core.wmem_default = $buf_def
net.ipv4.tcp_rmem = 4096 $buf_def $buf_max
net.ipv4.tcp_wmem = 4096 $buf_def $buf_max
# 全局 TCP 内存上限, 按物理内存推导. 设太高是小内存机 OOM 的主因.
net.ipv4.tcp_mem = $tcp_mem

net.ipv4.tcp_window_scaling = 1
net.ipv4.tcp_moderate_rcvbuf = 1
net.ipv4.tcp_adv_win_scale = 1

net.core.netdev_max_backlog = 16384
# netdev_budget 测过了, 【没有可测量的差别】, 所以维持原值.
# 唯一可信的那组是自有对端(4 台并发 3.75G, n=5, 变异系数 0%):
#   budget 600(现值) 3751 Mbps   budget 300(内核默认) 3745 Mbps   差 0.16%
#   两边 softnet_drop 都是 0, time_squeeze 相当.
#   数据: results/highbw-20260811/netdev-budget300-vs600-multisource.tsv
# ⚠ 别拿公共 10G 对端(speedtest.lax12)测这个 —— 那几组变异系数 23%~45%,
#   同一个变体既能跑 9395 也能跌到 2817, 噪声完全压过参数效应.
#   曾经据此误判成"600 有害", 复核后推翻. 要重测请用自有对端.
net.core.netdev_budget = 600
# netdev_budget_usecs 不设, 用内核默认: 2 个时钟 tick (HZ=1000 是 2000, HZ=250 是 8000).
#   以前写死 4000, 在 HZ=250 的内核(Debian 全系等)上等于把默认值砍半;
#   6.1.130+/6.6.84+/6.12.20+/6.14+ 不允许低于 2 个 tick, 直接拒绝 (issue #9).
net.core.optmem_max = 65536
net.core.somaxconn = 8192
net.ipv4.tcp_max_syn_backlog = 8192

net.ipv4.tcp_slow_start_after_idle = 0
net.ipv4.tcp_no_metrics_save = 0
net.ipv4.tcp_mtu_probing = 1
net.ipv4.tcp_sack = 1
net.ipv4.tcp_dsack = 1
net.ipv4.tcp_timestamps = 1
net.ipv4.tcp_fastopen = 3

net.ipv4.tcp_syncookies = 1
net.ipv4.tcp_tw_reuse = 1
net.ipv4.tcp_fin_timeout = 15
net.ipv4.tcp_keepalive_time = 600
net.ipv4.ip_local_port_range = 1024 65535

vm.min_free_kbytes = 32768
fs.file-max = 1000000

# 刻意不设的项:
#   tcp_notsent_lowat  —— 低核数机器上压吞吐
#   tcp_reordering=300 —— 现代内核走 RACK, 调高只推迟快速重传
# tcpfit-end
EOF
  then
    rm -f "$_sc_tmp"
    die "写入 $SYSCTL_FILE 失败（磁盘满? 配额/文件大小限制?）, 原配置未改动" 1
  fi
  # 哨兵行: cat 即使被信号打断也可能返回 0, 用结尾标记确认内容完整
  if ! tail -1 "$_sc_tmp" | grep -qx '# tcpfit-end'; then
    rm -f "$_sc_tmp"
    die "开机配置写入不完整（被截断）, 原配置未改动" 1
  fi
  # mktemp 建出来是 0600, 直接改名过去 sysctl.d 里就只有它是 600 ——
  # 和 install.sh 的 0711 是同一个坑. 存档恢复那条路径本来就有 chmod 644.
  chmod 644 "$_sc_tmp" && mv -- "$_sc_tmp" "$SYSCTL_FILE" || {
    rm -f "$_sc_tmp"; die "开机配置落盘失败, 原配置未改动" 1; }

  # 逐项校验并把内核拒绝的项注释掉.
  #
  # 为什么不解析 `sysctl -p` 的报错文本: 格式随 procps 版本和 locale 变
  # （"cannot stat /proc/sys/..." / "setting key \"net.core.x\"" / 本地化过的消息），
  # 实测非 ASCII 键名还会把正则截断. 直接试写最可靠.
  #
  # 为什么必须注释掉而不只是警告: 被拒的项留在文件里, systemd-sysctl.service
  # 每次开机都会 failed（issue #9: Debian 13 / 6.12 拒绝 netdev_budget_usecs=4000）.
  # 用户看到的是一个红色的系统服务, 而不是"某个参数没生效".
  local _bad=0 _line _k _v _path
  while IFS= read -r _line; do
    case "$_line" in \#*|'') continue ;; esac
    case "$_line" in *=*) ;; *) continue ;; esac
    _k=$(printf '%s' "${_line%%=*}" | tr -d ' \t')
    _v=$(printf '%s' "${_line#*=}" | sed 's/^[[:space:]]*//; s/[[:space:]]*$//')
    [ -n "$_k" ] || continue
    _path="/proc/sys/$(printf '%s' "$_k" | tr '.' '/')"
    if [ ! -e "$_path" ] || ! sysctl -qw "$_k=$_v" 2>/dev/null; then
      # 用固定分隔符 | 之外的字符做 sed 分隔, 键名里不会有 %
      sed -i "s%^[[:space:]]*${_k}[[:space:]]*=%# 本内核不支持, tcpfit 自动注释: ${_k} =%" "$SYSCTL_FILE"
      warn "  内核不接受  ${_k} = ${_v}  —— 已在配置里注释掉"
      _bad=$(( _bad + 1 ))
    fi
  done < "$SYSCTL_FILE"
  # 读回确认: 哨兵行必须还在（上面的 sed 注释操作也可能写坏文件）
  tail -1 "$SYSCTL_FILE" 2>/dev/null | grep -qx '# tcpfit-end' ||
    die "开机配置 $SYSCTL_FILE 不完整, 基础调优未完成" 1

  if [ "$_bad" -gt 0 ]; then
    ok "sysctl applied: $SYSCTL_FILE（$_bad 项被本内核拒绝, 已注释, 不影响其他调优）"
  else
    ok "sysctl applied: $SYSCTL_FILE"
  fi

  if [ "$no_initcwnd" = 0 ]; then
    local _rt_ok=0
    if route_set_initcwnd 32; then
      _rt_ok=1
      mkdir -p "$STATE_DIR"; : > "$INITCWND_MARKER"
      printf 'initcwnd 32 initrwnd 32\n' > "$INITCWND_VALS"
      ok "initcwnd/initrwnd = 32"
    else
      if [ -z "$(ip -4 route show default 2>/dev/null)" ]; then
        warn "initcwnd 未设置: 主表里没有默认路由（策略路由 / 双 /1 路由）"
      else
        warn "initcwnd not applied (unsupported on some hypervisors)"
      fi
    fi
    # 持久化路按可靠性挑一条, 至少要落一条; 全落不上必须明说 ——
    # 早期版本两条都不命中时一声不响, 用户以为设好了, 重启就丢.
    # 而 sysctl 和整形是持久的, 只有路由窗口这一项会丢 —— 差别用户看不出来.
    # 网卡按 route_set_initcwnd 实际改的那条路由取(主表第一条 default),
    # 不按测速目标取 —— 多出口机器上两者可能不同, 持久化要补的是改过的那条.
    local _wif; _wif=$(route_field dev); [ -n "$_wif" ] || _wif="$iface"
    if persist_initcwnd "$_wif"; then
      info "initcwnd 开机持久化: ${INITCWND_VIA}"
      case "$INITCWND_VIA" in systemd-networkd*)
        # 运行时没设上(上面已经报了)就不能说"已带上"
        if [ "$_rt_ok" = 1 ]; then echo "  现在的路由已带上窗口."
        else echo "  当前路由没能设上窗口(见上面的提示), networkd 下次装路由时会自带."; fi
        networkd_pending_note ;;
      esac
    else
      warn "initcwnd 只在本次生效, 重启后会丢 —— 本机没有可用的持久化入口."
      echo "  已尝试: pppd 钩子、systemd-networkd、systemd 单元, 这台都用不了."
      echo "  注意: sysctl（BBR / 缓冲区）和整形是持久的, 只有路由窗口这一项会丢."
    fi
  elif clear_owned_initcwnd; then
    [ "${INITCWND_CLEARED:-0}" = 1 ] && ok "Low-bandwidth path: tcpfit initcwnd override removed"
  else
    warn "无法清除旧 initcwnd；已移除持久化 hook，当前路由请手工检查"
  fi

  # 存一份调优后的存档. 参数一起记进去, 以后翻存档能看出当时是按什么调的.
  ARCH_ROLE="$role"; ARCH_BW="$bw"; ARCH_RTT="$rtt"; ARCH_PEER="$peer"
  # 向导还要扫描并决定最终整形, 存档留到全部完成之后, 避免记录中间状态.
  [ "$WIZARD" = 1 ] && return 0
  archive_save "${ARCH_SAVE_NAME:-base-${bw}M-rtt${rtt}}" >/dev/null 2>&1 ||
    warn "调优已生效, 但存档没建成 —— rollback 仍可用($(disp) rollback), 存档功能可稍后手动 $(disp) archive save"

  info "基础调优完成. 下一步跑 sweep 找限速器拐点 —— 那才是大头."
  echo "  $(disp) sweep --peer <近处的iperf3服务器> --nominal $bw"

  # 小内存机不加 swap 就是定时炸弹：实测过 tcp_mem 撑爆内存把代理进程连杀 7 次
  if [ "$ram" -le 1024 ] && ! not_blank "$(swapon --show 2>/dev/null)"; then
    echo
    warn "本机内存 ${ram}MB 且无 swap, 跑代理建议加一个：$(disp) harden --swap 2G"
  fi
}

# ── 系统加固 ────────────────────────────────────────────────────────────────
# 与网络参数无关, 但小内存机不加 swap 就没有任何缓冲余地：TCP 缓冲区一涨,
# 内核直接杀进程. 表现是"测速跑一半掉速", 要翻 journalctl 才看得出来.
cmd_harden(){
  need_root
  take_lock
  local swap_size=""
  while [ $# -gt 0 ]; do
    case "$1" in
      --swap) swap_size="$2"; shift 2 ;;
      *) die "未知参数: $1" ;;
    esac
  done
  [ -n "$swap_size" ] || die "需要 --swap <大小>, 例如 --swap 2G 或 --swap 2"

  # 单位一律按 GB. "2" 和 "2G" 都收 —— 向导和文档里写的都是 2G, 只收纯数字
  # 会把它们全挡掉（v0.4.3 就是这样, 向导结尾的 swap 提示按 y 之后直接 die 退出）.
  # 但不收 "2M": fallocate 会建 2MB, 而失败回退的 dd 建 2GB, 两条路差 1000 倍.
  local gb="${swap_size%[Gg]}"
  is_posint "$gb" 1 20 || die "swap 大小请填 1-20 之间的整数, 单位 GB（例如 2 或 2G）"
  swap_size="${gb}G"

  # 校验通过再存快照, 打错参数不该留下状态
  take_snapshot        # harden 会往 $SYSCTL_FILE 追加 vm.swappiness,
                       # 不存快照的话之后跑 tune 会因"有配置无快照"直接中止

  if not_blank "$(swapon --show 2>/dev/null)"; then
    info "已有 swap, 跳过: $(free -h | awk '/Swap/{print $2}')"
    return 0
  fi
  # 已存在但没启用的 /swapfile 不能盖 —— 那可能是用户自己准备的
  [ -e /swapfile ] && die "/swapfile 已存在但未启用. 先确认它的用途, 需要的话手动删除后再跑"
  info "创建 ${swap_size} swap…"
  fallocate -l "$swap_size" /swapfile 2>/dev/null \
    || dd if=/dev/zero of=/swapfile bs=1M count=$(( gb * 1024 )) status=none
  chmod 600 /swapfile && mkswap /swapfile >/dev/null && swapon /swapfile \
    || die "swap 创建失败"
  grep -q '^/swapfile' /etc/fstab || echo '/swapfile none swap sw 0 0' >> /etc/fstab
  # 记下这个 swapfile 是 tcpfit 建的; --purge-swap 只删有这个标记的
  mkdir -p "$STATE_DIR"; : > "$STATE_DIR/swapfile.owned"
  ok "swap 已启用: $(free -h | awk '/Swap/{print $2}')"
  # 只在内存真的紧张时才用 swap, 避免平时把热数据换出去拖慢代理
  sysctl -qw vm.swappiness=10
  grep -q '^vm.swappiness' "$SYSCTL_FILE" 2>/dev/null || echo "vm.swappiness = 10" >> "$SYSCTL_FILE"
  ok "vm.swappiness = 10"
}

# ── 出向整形 ────────────────────────────────────────────────────────────────
# HTB 做全局上限（多流场景必需）, fq 叶子做 hrtimer 逐包 pacing.
# burst 取 4ms 的线速数据量：避免高带宽下 32k 太浅, 同时仍限制微突发.
qdisc_root_kind(){   # qdisc_root_kind <iface>
  local out
  out=$(tc qdisc show dev "$1" 2>/dev/null)
  awk '$1=="qdisc"{for(i=1;i<=NF;i++) if($i=="root"){print $2; exit}}' <<<"$out"
}

# 内核自动创建的多队列根 qdisc 常显示为 `mq 0:`. handle 0 不能直接删除，
# 会报 "Cannot delete qdisc with handle of zero". 先把同一个 mq 换成普通句柄，
# 再删除；其他 qdisc 仍走一次普通 del.
qdisc_remove_root(){   # qdisc_remove_root <iface>
  local iface="$1" handle
  tc qdisc del dev "$iface" root 2>/dev/null && return 0
  # 删根失败有三种情况, 早期版本把后两种一律当失败:
  #   a) 真失败
  #   b) 根是 mq 且句柄为 0 —— 先给它一个真句柄才删得掉
  #   c) 根是内核开机自动装的默认 qdisc, 句柄就是 0 ——
  #      它【删不掉, 但能被直接覆盖】. 实测句柄 0 的根上 add / replace 都 rc=0.
  # 把 c 当失败的代价: 一台刚开机、default_qdisc 还是 fq_codel 的机器
  # （Ubuntu/Debian 出厂就是这样）跑 sweep 会停在
  # "failed to enable fq for unshaped probe", 而它其实什么毛病都没有.
  if [ "$(qdisc_root_kind "$iface")" = mq ]; then
    tc qdisc replace dev "$iface" root handle 1: mq 2>/dev/null || return 1
    tc qdisc del dev "$iface" root 2>/dev/null
    return $?
  fi
  handle=$(tc qdisc show dev "$iface" 2>/dev/null |
           awk '$1=="qdisc"{for(i=1;i<=NF;i++) if($i=="root"){print $3; exit}}')
  # 句柄 0 = 内核自己装的, 没什么可删, 交给调用方覆盖
  [ "$handle" = "0:" ] && return 0
  return 1
}

qdisc_set_mq_leaves(){   # qdisc_set_mq_leaves <iface> <kind>
  local iface="$1" kind="$2" out handle major parents p
  out=$(tc qdisc show dev "$iface" 2>/dev/null)
  handle=$(awk '$1=="qdisc" && $2=="mq"{
    for(i=1;i<=NF;i++) if($i=="root"){print $3; exit}}' <<<"$out")
  major=${handle%:}
  # 内核自建的 mq 句柄是 `0:`, 它的叶子在 tc 里显示成 `parent :N`,
  # 但那个写法【无法用来寻址】—— 真机实测(104.250, 8 队列 10G):
  #   tc qdisc replace dev eth0 parent :1  fq  →  Error: Failed to find specified qdisc.
  #   tc qdisc replace dev eth0 parent 0:1 fq  →  同样失败
  # 而真网卡开机后的默认状态就是 `mq 0:`, 所以不先给句柄的话这个函数在实机上必定失败.
  # 先 `replace root handle 1: mq`(只改句柄, 叶子原样保留), 叶子变成 `parent 1:N` 才能换.
  if [ -z "$major" ] || [ "$major" = 0 ]; then
    tc qdisc replace dev "$iface" root handle 1: mq 2>/dev/null || return 1
    out=$(tc qdisc show dev "$iface" 2>/dev/null)
    handle=$(awk '$1=="qdisc" && $2=="mq"{
      for(i=1;i<=NF;i++) if($i=="root"){print $3; exit}}' <<<"$out")
    major=${handle%:}
  fi
  [ -n "$major" ] || return 1
  parents=$(awk -v m="$major" '$1=="qdisc" && $0 ~ / parent /{
    for(i=1;i<=NF;i++) if($i=="parent"){
      p=$(i+1)
      if((m=="0" && (p ~ /^:/ || index(p,"0:")==1)) || (m!="0" && index(p,m ":")==1)) print p
      break}}
  ' <<<"$out")
  [ -n "$parents" ] || return 1
  for p in $parents; do
    tc qdisc replace dev "$iface" parent "$p" "$kind" 2>/dev/null || return 1
  done
}

qdisc_is_fq(){   # root fq，或 mq 下所有硬件队列均为 fq
  local out root handle major
  out=$(tc qdisc show dev "$1" 2>/dev/null)
  root=$(awk '$1=="qdisc"{for(i=1;i<=NF;i++) if($i=="root"){print $2; exit}}' <<<"$out")
  [ "$root" = fq ] && return 0
  [ "$root" = mq ] || return 1
  handle=$(awk '$1=="qdisc" && $2=="mq"{
    for(i=1;i<=NF;i++) if($i=="root"){print $3; exit}}' <<<"$out")
  major=${handle%:}
  [ -n "$major" ] || return 1
  awk -v m="$major" 'BEGIN{leaves=0; bad=0}
       $1=="qdisc" && $0 ~ / parent /{
         for(i=1;i<=NF;i++) if($i=="parent"){
           p=$(i+1)
           if((m=="0" && (p ~ /^:/ || index(p,"0:")==1)) || (m!="0" && index(p,m ":")==1)){
             leaves++; if($2!="fq") bad=1
           }
           break
         }
       }
       END{exit !(leaves>0 && bad==0)}' <<<"$out"
}

# mq 是网卡的硬件多队列结构，不能为了启用 fq 把它压成单队列 root fq.
# 保留 mq 根，只替换每个叶子；普通网卡则安装 root fq.
qdisc_set_fq(){   # qdisc_set_fq <iface>
  local iface="$1" kind
  kind=$(qdisc_root_kind "$iface")
  case "$kind" in
    mq) qdisc_set_mq_leaves "$iface" fq || return 1 ;;
    fq) : ;;
    ""|noqueue) tc qdisc add dev "$iface" root fq 2>/dev/null || return 1 ;;
    *) qdisc_remove_root "$iface" || return 1
       # 删掉非 fq 的根之后【要重新看一眼内核装了什么】.
       # 多队列网卡上内核会自动补回 mq + 每队列的 default_qdisc ——
       # 真机实测(104.250, 8 队列 10G): 装上 HTB 再 `tc qdisc del root`,
       # 立刻变成 `qdisc mq 0: root` + 8 个 fq 叶子.
       # 这时候直接 `add root fq` 会把 mq 压成单根 fq, 8 个硬件队列退化成一把锁 ——
       # 正是本版修 mq 想避免的事. cmd_shape --off 走的就是这条分支.
       kind=$(qdisc_root_kind "$iface")
       case "$kind" in
         mq) qdisc_set_mq_leaves "$iface" fq || return 1 ;;
         fq) : ;;
         *)  tc qdisc replace dev "$iface" root fq 2>/dev/null || return 1 ;;
       esac ;;
  esac
  qdisc_is_fq "$iface"
}

# pppd 每次拨通都会遍历执行 /etc/ppp/ip-up.d/. PPP 接口重拨后是新接口,
# qdisc 和路由窗口都会丢, 而 tcpfit-qdisc.service 是 oneshot 只在开机跑一次,
# networkd-dispatcher 又管不到 pppd 拉起的接口.
# 【整形和 initcwnd 都要靠它】—— 所以 cmd_tune 设了 initcwnd 也要装,
# 不能只在应用整形时装: 线路没有限速器时不整形, 但 initcwnd 照样需要恢复.
# 钩子本身两个守卫都会自检, 对应产物不在时是空操作.
# 判断一块网卡是不是真的 PPP 设备. ARPHRD_PPP = 512（内核常量,
# include/uapi/linux/if_arp.h）, 比按 "ppp*" 猜名字可靠 ——
# OpenWrt 叫 pppoe-wan, 而别的东西也可能叫 pppx.
iface_is_ppp(){ [ "$(cat "/sys/class/net/$1/type" 2>/dev/null)" = 512 ]; }

# 开机把路由窗口写回去. 给"有 systemd 但网不归 systemd-networkd 管"的机器用
# (ifupdown / NetworkManager). 用单元的代价: 它只在开机跑一次, 中途换 IP/重连
# 不会跟上 —— 所以排在 PPP 钩子和 networkd 两条路之后, 只是兜底(见 persist_initcwnd).
write_initcwnd_unit(){   # write_initcwnd_unit <网卡名> [窗口 token...]
  local iface="$1"; shift
  local -a win=("$@")
  if [ "${#win[@]}" = 0 ] && [ -s "$INITCWND_VALS" ]; then
    read -r -a win < "$INITCWND_VALS"
  fi
  [ "${#win[@]}" -gt 0 ] || win=(initcwnd 32 initrwnd 32)
  command -v systemctl >/dev/null 2>&1 || return 1
  [ -d "$(dirname "$INITCWND_UNIT")" ] || return 1
  mkdir -p "$(dirname "$INITCWND_SCRIPT")" 2>/dev/null || return 1
  {
    printf '#!/bin/sh\n'
    printf '# tcpfit: 开机写回路由窗口. 沿用现有路由的全部 token, 只换窗口字段 ——\n'
    printf '# 自己拼 `via X dev Y` 会丢掉 onlink / metric / proto / src.
# 实例: 一台香港机器的路由是 `default via 192.168.146.1 dev eth0 onlink`
# —— 服务商 cloud-init 下发的, 拼不出来.\n'
    printf 'R=$(ip -4 route show default 2>/dev/null |\n'
    printf '    awk -v d=%s \x27{for(i=1;i<NF;i++) if($i=="dev" && $(i+1)==d){print; exit}}\x27)\n' \
           "$(printf '%q' "$iface")"
    printf '[ -n "$R" ] || exit 0\n'
    cat <<'IH'
C=$(printf '%s' "$R" | awk '{o="";for(i=1;i<=NF;i++){if($i=="initcwnd"||$i=="initrwnd"){i++;continue};o=o" "$i};print o}')
IH
    printf '[ -n "$C" ] && ip -4 route replace $C%s 2>/dev/null\n' "$(printf ' %s' "${win[@]}")"
    printf 'exit 0\n'
  } > "$INITCWND_SCRIPT" || return 1
  chmod 755 "$INITCWND_SCRIPT" || return 1
  cat > "$INITCWND_UNIT" <<EOF || return 1
[Unit]
Description=tcpfit initcwnd/initrwnd on the default route
After=network-online.target
Wants=network-online.target
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=$INITCWND_SCRIPT
[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload >/dev/null 2>&1 || true
  # 写出来不等于会开机跑 —— 必须 enable 成功才算持久化.
  # 但要把"写不出去"和"写了 systemd 不收"分开说: 后者文件还在, 用户可以手动
  # systemctl enable; 笼统说"没有可用的持久化入口"会让人以为白干了.
  if systemctl enable --now tcpfit-initcwnd.service >/dev/null 2>&1 &&
     systemctl is-enabled tcpfit-initcwnd.service >/dev/null 2>&1; then
    return 0
  fi
  warn "已生成 $INITCWND_UNIT, 但 systemd 没接受它."
  echo "  手动启用:  systemctl enable --now tcpfit-initcwnd.service"
  return 1
}

write_ppp_hook(){   # write_ppp_hook <网卡名> [窗口 token...]
  # 网卡名显式传进来, 不靠 bash 动态作用域去蹭调用方的 local iface ——
  # 那种隐式依赖一改调用方就静默失效.
  # 窗口值也要能指定: 恢复存档时用的是存档里的值, 不一定是 32.
  local iface="$1"; shift
  local -a win=("$@")
  # 没显式给窗口值时, 优先读上次实际写入的那份, 再退回 32/32.
  if [ "${#win[@]}" = 0 ] && [ -s "$INITCWND_VALS" ]; then
    read -r -a win < "$INITCWND_VALS"
  fi
  [ "${#win[@]}" -gt 0 ] || win=(initcwnd 32 initrwnd 32)
  [ -n "$iface" ] || return 1
  # 两个条件缺一不可, 返回 1 让调用方知道这条持久化路径不可用,
  # 好去试别的（networkd-dispatcher）, 而不是当成已经持久化了.
  #   1) 目录存在（取 dirname 不写死路径, 和 ROUTE_HOOK 一致, 测试才能重定向）
  #   2) 【这块网卡确实是 PPP 设备】—— 只看目录不够: 装了 ppp 包(比如为了
  #      跑 PPTP/L2TP 客户端)的普通机器也有这个目录, 但它的 eth0 永远不会
  #      触发 pppd 的 ip-up 事件. 早期版本只查目录, 于是在这种机器上
  #      回报"窗口已持久化交给 pppd", 实际重启就没了.
  [ -d "$(dirname "$PPP_HOOK")" ] || return 1
  iface_is_ppp "$iface" || return 1
  {
    printf '#!/bin/sh\n'
    printf '# tcpfit: pppd 每次拨通后执行, $1 = 接口名.\n'
    printf '# 用 $1 而不是自己查路由 —— 此刻默认路由未必已经装好.\n'
    printf '# 只认调优时那块网卡: 机器上可能还有别的 ppp 链路(PPTP/L2TP VPN 之类),\n'
    printf '# 不加这道判断的话, VPN 一连上就会被套上给 WAN 算的限速值.\n'
    printf '[ "$1" = %s ] || exit 0\n' "$(printf '%q' "$iface")"
    printf '[ -x %s ] && TCPFIT_IF="$1" %s >/dev/null 2>&1\n' \
           "$(printf '%q' "$QDISC_SCRIPT")" "$(printf '%q' "$QDISC_SCRIPT")"
    printf '# initcwnd: 沿用新路由的全部 token, 只补窗口字段（只在 tcpfit 设过时才做）\n'
    printf 'if [ -f %s ]; then\n' "$(printf '%q' "$INITCWND_MARKER")"
    printf '# 必须按 $1 筛选默认路由, 不能取第一条 —— 多出口机器上第一条\n'
    printf '# 可能是别的网卡(metric 更低), 那样会给 eth1 加窗口, ppp0 反而没加.\n'
    cat <<'H'
  R=$(ip -4 route show default 2>/dev/null |
      awk -v d="$1" '{for(i=1;i<NF;i++) if($i=="dev" && $(i+1)==d){print; exit}}')
  if [ -n "$R" ]; then
    C=$(printf '%s' "$R" | awk '{o="";for(i=1;i<=NF;i++){if($i=="initcwnd"||$i=="initrwnd"){i++;continue};o=o" "$i};print o}')
H
    printf '    [ -n "$C" ] && ip -4 route replace $C%s 2>/dev/null\n' "$(printf ' %s' "${win[@]}")"
    printf '  fi\nfi\nexit 0\n'
  } > "$PPP_HOOK" || return 1
  chmod 755 "$PPP_HOOK"
}

# systemd-networkd 是否真的在管这块网卡. 只看 /etc/networkd-dispatcher 在不在不够:
# Ubuntu 默认装着 networkd-dispatcher, 网却可能是 ifupdown / NetworkManager 管的 ——
# 那样钩子永远不会被触发, 而早期版本据此报"已持久化", 单元兜底也被跳过.
networkd_manages(){   # networkd_manages <网卡>
  [ -n "${1:-}" ] || return 1
  command -v networkctl >/dev/null 2>&1 || return 1
  systemctl is-active -q systemd-networkd 2>/dev/null || return 1
  # configuring 也算: networkd 正在(重新)配置这块网卡, 同样是它在管.
  # 只认 configured 时容器实测偶发落空 —— 赶上 networkd 刚好在重配, 就退到了只在开机跑的单元.
  # 不能写成 `networkctl status | grep -q`: grep 一匹配就退出关管道, 真机上 status 输出很长
  # (末尾还带日志), networkctl 往关掉的管道里写被 SIGPIPE 杀掉(141), pipefail 下整条判失败.
  # 容器里输出短测不出来, 本机实跑才暴露 —— 先存变量再查.
  local st
  st=$(networkctl status "$1" 2>/dev/null) || return 1
  grep -Eq 'State: [a-z-]+ \((configured|configuring)' <<< "$st"
}

# 把窗口写进 networkd 的 drop-in, 让 DHCP 下发的默认路由自带. 三个前提缺一不可:
#   1) systemd >= 255 —— [DHCPv4] 里的 InitialCongestionWindow= 是 255 才加的,
#      249(Ubuntu 22.04)会报 Unknown key 并忽略
#   2) 当前默认路由是 DHCP 下发的(proto dhcp). 静态 Gateway= 要改 [Route] 段,
#      等于改写用户自己的路由定义, 不碰
#   3) 找得到这块网卡用的 .network 文件(netplan 生成在 /run/systemd/network)
# 不 reload networkd: reload 会重配网卡, 生产机上不值得冒这个险. 运行时的窗口
# 调用方已经直接设好了, drop-in 管的是 networkd 下一次(重启 / 续租 / 开机)装路由.
write_networkd_dropin(){   # write_networkd_dropin <网卡> [窗口 token...]
  local iface="$1"; shift
  local ver nf dir route cw="" rw=""
  ver=$(systemctl --version 2>/dev/null | awk 'NR==1{print $2+0}')
  [ "${ver:-0}" -ge 255 ] 2>/dev/null || return 1
  route=$(ip -4 route show default 2>/dev/null |
          awk -v d="$iface" '{for(i=1;i<NF;i++) if($i=="dev" && $(i+1)==d){print; exit}}')
  case " $route " in *" proto dhcp "*) ;; *) return 1 ;; esac
  nf=$(networkctl status "$iface" 2>/dev/null) || return 1      # 同 networkd_manages: 先存再查
  nf=$(awk -F': ' '/Network File:/{print $2; exit}' <<< "$nf")
  case "$nf" in /*.network) ;; *) return 1 ;; esac
  while [ $# -ge 2 ]; do
    case "$1" in initcwnd) cw="$2" ;; initrwnd) rw="$2" ;; esac
    shift 2
  done
  [ -n "$cw$rw" ] || return 1
  dir="$NETWORKD_DIR/${nf##*/}.d"
  mkdir -p "$dir" 2>/dev/null || return 1
  {
    printf '# tcpfit: networkd 每次装 DHCP 默认路由时自带窗口. 删掉这个文件即还原.\n'
    printf '[DHCPv4]\n'
    if [ -n "$cw" ]; then printf 'InitialCongestionWindow=%s\n' "$cw"; fi
    if [ -n "$rw" ]; then printf 'InitialAdvertisedReceiveWindow=%s\n' "$rw"; fi
  } > "$dir/$INITCWND_DROPIN_NAME" || { rm -f "$dir/$INITCWND_DROPIN_NAME"; return 1; }
  INITCWND_DROPIN_PATH="$dir/$INITCWND_DROPIN_NAME"
}

# networkd-dispatcher 钩子: 网卡变成 routable 时写回窗口. 只认调优时那块网卡,
# 沿用现有路由的全部 token, 只换窗口字段（和单元脚本同一套写法）.
write_dispatcher_hook(){   # write_dispatcher_hook <网卡> [窗口 token...]
  local iface="$1" tmp; shift
  [ -d "$(dirname "$ROUTE_HOOK")" ] || return 1
  tmp=$(mktemp "${ROUTE_HOOK}.XXXXXX" 2>/dev/null) || return 1
  {
    printf '#!/bin/sh\n'
    printf '# tcpfit: 网卡变成 routable 时写回路由窗口. 沿用现有路由的全部 token, 只换窗口字段.\n'
    printf 'R=$(ip -4 route show default 2>/dev/null |\n'
    printf '    awk -v d=%s \x27{for(i=1;i<NF;i++) if($i=="dev" && $(i+1)==d){print; exit}}\x27)\n' \
           "$(printf '%q' "$iface")"
    printf '[ -n "$R" ] || exit 0\n'
    cat <<'IH'
C=$(printf '%s' "$R" | awk '{o="";for(i=1;i<=NF;i++){if($i=="initcwnd"||$i=="initrwnd"){i++;continue};o=o" "$i};print o}')
IH
    printf '[ -n "$C" ] && ip -4 route replace $C%s 2>/dev/null\n' "$(printf ' %s' "$@")"
    printf 'exit 0\n'
  } > "$tmp" || { rm -f "$tmp"; return 1; }
  chmod 755 "$tmp" && mv -- "$tmp" "$ROUTE_HOOK" || { rm -f "$tmp"; return 1; }
}

# 钩子写了不等于有人执行. Ubuntu 的 networkd-dispatcher 单元带
# ConditionPathExistsGlob=|/etc/networkd-dispatcher/*/*: 开机时一个钩子都没有就被跳过,
# 之后一直不跑 —— 实测本机 9/27 写的钩子到 9/30 窗口被冲掉都没执行过.
# 现在钩子有了, 把它拉起来; 管理员禁用了它就不碰, 换别的路.
dispatcher_running(){
  systemctl is-enabled -q networkd-dispatcher 2>/dev/null || return 1
  systemctl start networkd-dispatcher >/dev/null 2>&1 || true
  systemctl is-active -q networkd-dispatcher 2>/dev/null
}

# 撤掉路由窗口的开机持久化入口: dispatcher 钩子、tcpfit-initcwnd 单元和脚本、networkd drop-in.
# PPP 钩子不在这里删 —— 它还负责重拨后重建整形, 删不删由调用方定.
# 删过 drop-in 会置 INITCWND_DROPIN_REMOVED=1(不在这里清零: 存档恢复会连调两次), 调用方据此提醒.
remove_initcwnd_persistence(){
  local f rc=0
  rm -f "$ROUTE_HOOK" "$INITCWND_SCRIPT" || rc=1
  # `|| true` 不能省 —— 单元不存在时 disable 返回非零, 调用方开了 set -e 就会中止.
  systemctl disable --now tcpfit-initcwnd.service >/dev/null 2>&1 || true
  rm -f "$INITCWND_UNIT" || rc=1
  for f in "$NETWORKD_DIR"/*.network.d/"$INITCWND_DROPIN_NAME"; do
    [ -e "$f" ] || continue
    rm -f "$f" || rc=1
    INITCWND_DROPIN_REMOVED=1
    rmdir "${f%/*}" 2>/dev/null || true     # 目录里还有用户自己的 drop-in 就留着
  done
  return "$rc"
}

# networkd 不会马上重读 drop-in(不 reload, 见 write_networkd_dropin). 写、改、删之后提醒一句.
# 说"可能": 重跑 tune 写的是同一份内容时, networkd 可能早就载入过了.
networkd_pending_note(){
  echo "  networkd 没有重载(重载会短暂重配网卡), 下次重启或开机后才完全按新配置;"
  echo "  在那之前网卡断开重连时, 路由上的窗口可能暂时不对."
}

# 路由窗口的开机持久化. 按可靠性挑一条, 先清掉其他路留下的入口 ——
# 切换方式时旧入口还在的话, 开机时两边各写一遍, 谁后跑谁赢.
#   1) PPP 网卡                          → pppd 的 ip-up 钩子(每次拨通都跑)
#   2) networkd 管的 DHCP 网卡, systemd >= 255 → .network drop-in(networkd 重启 / 续租都不丢)
#   3) networkd 管的其他网卡              → dispatcher 钩子, 且 dispatcher 必须真的在跑.
#      已知缺口: networkd 重启时链路一直是 routable, 钩子不触发, 要到网卡重连或开机才补回
#   4) 其他有 systemd 的机器              → tcpfit-initcwnd.service, 只在开机跑
# tune 和存档恢复共用这一套 —— 两边各写一份时已经分歧过两次.
persist_initcwnd(){   # persist_initcwnd <网卡> [窗口 token...] ; 成功时 INITCWND_VIA 写明走的哪条
  local iface="$1"; shift
  local -a win=("$@")
  [ "${#win[@]}" -gt 0 ] || win=(initcwnd 32 initrwnd 32)
  INITCWND_VIA=""
  remove_initcwnd_persistence || true
  if write_ppp_hook "$iface" "${win[@]}"; then
    INITCWND_VIA="pppd 的 ip-up 钩子"; return 0
  fi
  if networkd_manages "$iface"; then
    if write_networkd_dropin "$iface" "${win[@]}"; then
      INITCWND_VIA="systemd-networkd 配置 $INITCWND_DROPIN_PATH"; return 0
    fi
    if write_dispatcher_hook "$iface" "${win[@]}" && dispatcher_running; then
      INITCWND_VIA="networkd-dispatcher 钩子"; return 0
    fi
    rm -f "$ROUTE_HOOK"      # 没人执行的钩子别留着冒充持久化
  fi
  if write_initcwnd_unit "$iface" "${win[@]}"; then
    INITCWND_VIA="tcpfit-initcwnd.service（只在开机时执行）"; return 0
  fi
  return 1
}

write_qdisc(){
  local rate="$1" iface="$2"
  cat > "$QDISC_SCRIPT" <<EOF
#!/bin/bash
# 网卡名动态探测. 写死的话, 服务商换宿主/换槽位导致网卡改名之后
# 脚本就一直 "Cannot find device" —— 用户只看到服务 failed, 看不出原因.
# 探测不到才回退到生成时的名字.
IF=\${TCPFIT_IF:-}
[ -n "\$IF" ] || IF=\$(ip -o -4 route show default 2>/dev/null | head -1 |
      awk '{for(i=1;i<NF;i++) if(\$i=="dev"){print \$(i+1); exit}}')
[ -n "\$IF" ] || IF=${iface}
RATE=\${1:-${rate}}
BURST=\$(awk -v r="\$RATE" 'BEGIN{v=r*500; if(v<32768)v=32768; printf "%d",v}')
# 删根失败有两种可能, 早期版本把它们混为一谈:
#   a) 本来就没有可删的根（开机时最常见, 内核补回的默认 qdisc 句柄是 0）
#   b) 根是 mq 且句柄为 0 —— 要先给它一个真句柄才删得掉
# 无脑走 (b) 的话, 单队列网卡上 \`tc qdisc replace ... root mq\` 会报
# "RTNETLINK answers: Operation not supported" 然后 exit 1,
# 于是 tcpfit-qdisc.service 每次开机都失败, 整形静默丢失（issue #7）.
if ! tc qdisc del dev \$IF root 2>/dev/null; then
  case "\$(tc qdisc show dev \$IF 2>/dev/null | head -1)" in
    *" mq "*) tc qdisc replace dev \$IF root handle 1: mq 2>/dev/null &&
              tc qdisc del dev \$IF root 2>/dev/null ;;
  esac
fi
# 一律用 replace 而不是 add —— add 在已有同句柄 qdisc 时报 "File exists",
# 让重复执行(手动重跑 / systemd 重启单元)变成失败.
tc qdisc replace dev \$IF root handle 1: htb default 10 || exit 1
tc class replace dev \$IF parent 1: classid 1:10 htb rate \${RATE}mbit ceil \${RATE}mbit burst \${BURST} cburst \${BURST} quantum 1514 || exit 1
tc qdisc replace dev \$IF parent 1:10 handle 10: fq limit 40960 flow_limit 8192 maxrate \${RATE}mbit || exit 1
EOF
  chmod +x "$QDISC_SCRIPT"
  # 非 ppp 机器上返回 1 是正常的（没有 /etc/ppp/ip-up.d）, 这里不关心成败.
  # 显式吞掉而不是靠"调用方没开 set -e" —— 后者一换环境就炸.
  write_ppp_hook "$iface" || true
  cat > "$QDISC_UNIT" <<EOF
[Unit]
Description=tcpfit egress shaper
After=network-online.target
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=$QDISC_SCRIPT $rate
[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload >/dev/null 2>&1
  # 用 --now 而不是只 enable：否则 tc 规则虽已生效, systemctl is-active 却显示
  # inactive, status 里看着像坏了. 让 unit 状态和实际状态一致.
  systemctl enable --now tcpfit-qdisc.service >/dev/null 2>&1
}

cmd_shape(){
  need_root
  take_lock
  local rate="" off=0 iface
  while [ $# -gt 0 ]; do
    case "$1" in
      --rate) rate="$2"; shift 2 ;;
      --off)  off=1; shift ;;
      *) die "未知参数: $1" ;;
    esac
  done
  iface=$(detect_iface)

  # 没有限速器可躲的机器（sweep 全程干净）, HTB 的硬上限只会限制自己 ——
  # iperf3 的 TCP payload 通常只有线速的 93-96%（协议开销, 不是 HTB 损耗）,
  # 没有上游 policer 时保留聚合硬上限没有收益.
  # 移除后根 qdisc 退回纯 fq：BBR 的逐 socket pacing 仍然生效, 只是没有聚合上限.
  # 注意不动 sysctl, 基础调优完整保留 —— 这是它和 rollback 的区别.
  if [ "$off" = 1 ]; then
    systemctl disable --now tcpfit-qdisc.service >/dev/null 2>&1
    rm -f "$QDISC_UNIT" "$QDISC_SCRIPT"
    systemctl daemon-reload >/dev/null 2>&1
    qdisc_set_fq "$iface" || { warn "整形已移除，但无法在 ${iface} 上启用 fq"; return 1; }
    ok "整形已移除, qdisc 恢复为纯 fq"
    info "BBR 的逐 socket pacing 仍然生效, 只是没有了聚合速率上限."
    info "基础调优（拥塞控制 / 缓冲区）未受影响."
    [ "$WIZARD" = 1 ] || tc qdisc show dev "$iface" | head -1
    return 0
  fi

  [ -n "$rate" ] || die "需要 --rate <Mbit>, 或用 --off 移除整形"
  # 校验必须在 take_snapshot 之前 —— 否则打错一个字就会留下快照和半截 qdisc
  is_posint "$rate" 1 100000 || die "--rate 必须是 1-100000 的整数（Mbit）"
  take_snapshot
  write_qdisc "$rate" "$iface"
  systemctl restart tcpfit-qdisc.service 2>/dev/null || "$QDISC_SCRIPT" "$rate"
  # 事后用 tc 核对, 不能只看命令有没有报错.
  # 【不能】直接 grep "rate ${rate}Mbit" —— tc 对除得尽的值会换成 Gbit 显示:
  #   --rate 500 → "rate 500Mbit"   --rate 1000 → "rate 1Gbit"
  #   --rate 5000 → "rate 5Gbit"    --rate 9171 → "rate 9171Mbit"(除不尽, 不换)
  # 于是整千的整形值会匹配失败, 整形明明生效却报
  # "shaping did not take effect" —— 而 1000/2000/10000 正是最常手输的值.
  # 改成读回来换算成 Mbit 再比, 留 1% 容差(tc 内部按字节/秒存, 换算有舍入).
  local applied
  applied=$(tc_rate_mbit "$(tc class show dev "$iface" 2>/dev/null)")
  if [ -n "$applied" ] && awk -v a="$applied" -v r="$rate" \
       'BEGIN{exit !(a > r*0.99 && a < r*1.01)}' 2>/dev/null; then
    ok "HTB ${rate} Mbit + fq leaf pacing on ${iface}"
  else
    warn "shaping did not take effect on ${iface} -- check: tc qdisc show dev ${iface}"
    return 1
  fi
  systemctl is-enabled tcpfit-qdisc.service >/dev/null 2>&1 \
    && ok "tcpfit-qdisc.service enabled (survives reboot)" \
    || warn "tcpfit-qdisc.service not enabled -- shaping will be lost on reboot"
  [ "$WIZARD" = 1 ] || tc class show dev "$iface"
}

# 测试期间的临时整形. 结构必须和 write_qdisc 生成的完全一致.
#
# 两个原因:
#   1) fq 的 maxrate 是【每条流】的上限, 不是聚合上限. 实测: fq maxrate 300mbit
#      跑 -P 1 得 283 Mbps, 跑 -P 4 得 1134 Mbps(约 4 倍). 只有 HTB 才是聚合限速.
#      早期版本用 fq maxrate 做限速, 于是 validate_peer(跑 -P 2)名义上限 40%
#      实际能冲到 80%, 可能撞上限速器再把丢包报成"链路本身有损".
#   2) 扫描用一种结构、最终应用另一种结构的话, 测出来的拐点对不上实际部署.
apply_test_shaper(){   # apply_test_shaper <iface> <rate_mbit>
  local iface="$1" rate="$2" burst
  burst=$(calc_burst "$rate")
  qdisc_remove_root "$iface" || return 1
  tc qdisc add dev "$iface" root handle 1: htb default 10 2>/dev/null || return 1
  tc class add dev "$iface" parent 1: classid 1:10 htb \
     rate "${rate}mbit" ceil "${rate}mbit" burst "$burst" cburst "$burst" quantum 1514 2>/dev/null || return 1
  tc qdisc add dev "$iface" parent 1:10 handle 10: fq \
     limit 40960 flow_limit 8192 maxrate "${rate}mbit" 2>/dev/null || return 1
}

# ── 测试用 qdisc 的保存与恢复 ────────────────────────────────────────────────
# probe / validate_peer / sweep 都要临时换掉根 qdisc. 早期版本恢复时一律装成 fq,
# 于是原来的 mq(多队列网卡的正常结构)、CAKE 等配置被永久吞掉且无提示.
# 现在完整记下原始根 qdisc, 结束时按原样恢复.
QSAVE_KIND=""; QSAVE_LEAF_KIND=""; QSAVE_IFACE=""
qdisc_save(){   # qdisc_save <iface>
  # 守卫必须放在这里, 不能只放 CLI 入口. 早期版本只在 cmd_probe / cmd_sweep
  # 开头调 qdisc_guard, 而【向导直接调 probe_bandwidth】绕过了它 ——
  # 于是外部 HTB 在"确认带宽"那一步就被破坏, 用户还没走到调优.
  # validate_peer 同理. qdisc_save 是这些路径的共同入口.
  [ "$QDISC_GUARD_DONE" = 1 ] || qdisc_guard "$1" || return 1
  local out handle major
  QSAVE_IFACE="$1"
  out=$(tc qdisc show dev "$1" 2>/dev/null)
  QSAVE_KIND=$(awk '$1=="qdisc"{for(i=1;i<=NF;i++) if($i=="root"){print $2; exit}}' <<<"$out")
  QSAVE_LEAF_KIND=""
  if [ "$QSAVE_KIND" = mq ]; then
    handle=$(awk '$1=="qdisc" && $2=="mq"{
      for(i=1;i<=NF;i++) if($i=="root"){print $3; exit}}' <<<"$out")
    major=${handle%:}
    QSAVE_LEAF_KIND=$(awk -v m="$major" '$1=="qdisc" && $0 ~ / parent /{
      for(i=1;i<=NF;i++) if($i=="parent"){
        p=$(i+1)
        if((m=="0" && (p ~ /^:/ || index(p,"0:")==1)) || (m!="0" && index(p,m ":")==1)){print $2; exit}
        break}}
    ' <<<"$out")
  fi
}
# 把本脚本起的 iperf3 全部收掉. 只杀自己的子进程, 不动用户手工跑的.
# BusyBox(Alpine) 的 timeout 没有 --foreground, pkill 没有 -g. 启动时探一次,
# 不支持就退回到能用的写法, 而不是让每次调用都报错.
TIMEOUT_FG=""
timeout --foreground 1 true >/dev/null 2>&1 && TIMEOUT_FG="--foreground"
# 不能靠"跑一次看退出码"判断: BusyBox 不认 -g 时也返回 1, 会被误判成支持.
# 改看帮助里有没有长选项 --pgroup —— BusyBox 压根不支持长选项.
PKILL_G=0
has_str "$(pkill --help 2>&1)" "--pgroup" && PKILL_G=1

# 收掉本脚本起的 iperf3. 优先按进程组匹配 —— iperf3 的父进程是 timeout 不是本脚本,
# 按 -P $$ 匹配不到. 只杀同组的, 不动用户手工跑的.
reap_iperf(){
  if [ "$PKILL_G" = 1 ]; then
    pkill -g $$ -x iperf3 2>/dev/null; pkill -g $$ -x timeout 2>/dev/null
  else
    pkill -P $$ -x iperf3 2>/dev/null; pkill -P $$ -x timeout 2>/dev/null
  fi
  return 0
}

qdisc_restore(){
  reap_iperf
  [ -n "$QSAVE_IFACE" ] || return 0
  qdisc_remove_root "$QSAVE_IFACE" 2>/dev/null || true
  if [ -x "$QDISC_SCRIPT" ]; then
    "$QDISC_SCRIPT" >/dev/null 2>&1 && return 0   # 同理, 不清 QSAVE_IFACE
  fi
  case "$QSAVE_KIND" in
    mq)
      # 有些驱动删除临时 HTB 后会自动重建 mq，有些不会；两种都兼容.
      [ "$(qdisc_root_kind "$QSAVE_IFACE")" = mq ] || \
        tc qdisc add dev "$QSAVE_IFACE" root mq 2>/dev/null || return 1
      [ -z "$QSAVE_LEAF_KIND" ] || \
        qdisc_set_mq_leaves "$QSAVE_IFACE" "$QSAVE_LEAF_KIND" 2>/dev/null || return 1
      ;;
    ""|noqueue|pfifo_fast) : ;;
    *) tc qdisc add dev "$QSAVE_IFACE" root "$QSAVE_KIND" 2>/dev/null ;;
  esac
  # ⚠ 这里【不能】清空 QSAVE_IFACE.
  # cmd_sweep 会调它两次: 不限速探测结束后一次, 扫描全部结束后一次.
  # 早期版本在这里清空, 于是第二次调用直接 return 0 什么都不做 ——
  # 扫描最后一档的 HTB 就留在网卡上, 而屏幕上还打印 "qdisc restored".
  # 实测: 香港 CN2 跑完 sweep 后机器上仍挂着 class htb rate 31Mbit.
  # 只在"扫描跑了但没定位到拐点"时暴露（找到拐点的话后面 cmd_shape 会覆盖掉）.
  # del + add 本身是幂等的, 重复调用无害, 所以不需要这个哨兵.
}
# 未知/自定义 qdisc 不是我们能原样重建的, 先问过用户
# 一次运行只问一次. 向导里 probe_bandwidth / validate_peer / cmd_sweep
# 会各自换一遍 qdisc, 每次都弹确认会把人问烦.
QDISC_GUARD_DONE=0

qdisc_guard(){   # qdisc_guard <iface>
  QDISC_GUARD_DONE=1
  local k; k=$(qdisc_root_kind "$1")
  # HTB 不能一概放行. qdisc_save 只记根 qdisc 的【类型】, 不记 class、filter、
  # 子队列和参数; 恢复时只能新建一个空 HTB 根. 所以别人配的 HTB（带 class
  # 限速、HFSC 分层、tc filter 分类）跑完探测就没了, 而且没有任何提示.
  # 实测: 探测前 htb 1: + class 1:10 rate 50Mbit + fq 10:,
  #       探测后只剩 htb 8005: default 0, class 为空, 50Mbps 限速失效.
  # 判据: $QDISC_SCRIPT 存在 = 这个 HTB 是 tcpfit 自己下发的, 能原样重建.
  if [ "$k" = htb ]; then
    [ -x "$QDISC_SCRIPT" ] && return 0
    warn "本机根 qdisc 是 htb, 但不是 tcpfit 下发的（找不到 $QDISC_SCRIPT）."
    warn "测试期间它会被临时替换, 而 tcpfit 只能恢复一个【空的】htb 根 ——"
    warn "你自己配的 class / filter / 子队列会丢失, 限速随之失效."
    warn "建议: 先自行备份 tc 配置（tc qdisc show / tc class show / tc filter show）."
    confirm "  仍要继续？" || return 1
    return 0
  fi
  case "$k" in
    ""|mq|fq|noqueue|pfifo_fast|fq_codel) return 0 ;;
  esac
  warn "本机根 qdisc 是 ${k}, 测试期间会被临时替换."
  warn "结束时只能恢复成 ${k} 的默认参数, 自己的调优配置会丢失."
  confirm "  继续？" || return 1
}

# ── 带宽探测 ────────────────────────────────────────────────────────────────
# 虚拟网卡读不到标称速率(/sys/class/net/*/speed 为 -1), 而用户未必记得买的是多少兆.
# 这里用带 pacing 的多流测试估一个可用带宽, 供 tune 推导 BDP.
# 注意：这只是"够用的估计", 真正的限速器拐点仍要靠 sweep 实测.
probe_bandwidth(){
  local peer="$1" iface="$2" dur="${3:-10}"
  qdisc_save "$iface" || { echo ""; return 1; }
  trap 'qdisc_restore; exit 130' INT TERM HUP
  # 用 fq 做 pacing 但不设上限: 既避免突发打穿限速器, 又能探到真实上限
  qdisc_set_fq "$iface" || { qdisc_restore; echo ""; return 1; }
  local res gp
  for a in 1 2 3; do res=$(run_iperf "$peer" "$dur" 4); [ -n "$res" ] && break; sleep 8; done
  trap - INT TERM HUP
  qdisc_restore
  [ -n "$res" ] || { echo ""; return 1; }
  # run_iperf 第三列是接收端实际送达量. 老版 iperf3 没给 receiver 汇总时
  # 第三列为空, 退回使用既有的发送端数字.
  gp=$(echo "$res" | awk '{print $3}')
  [ -n "$gp" ] || gp=$(echo "$res" | awk '{print $1}')
  # 取整粒度跟档位走. 早期版本无脑向上取整, 实测把 305Mbps 估成 350,
  # 导致 sweep 的扫描区间整体偏高 —— 所以要取整.
  # 但粒度不能一刀切 50: int(10/50+0.5)*50 = 0, 十兆小水管直接归零,
  # 向导会打印 "Measured ~0 Mbps" 然后 die "无法确定带宽". 客户实际踩过.
  # 归零阈值是 25 Mbps, 25-49 还会被高估最多一倍(27.8→50, 而真实容量约 15).
  awk -v g="$gp" 'BEGIN{
    if      (g < 50)  s = 1      # 小水管: 不取整, 差 1M 都是差
    else if (g < 200) s = 10
    else              s = 50     # 大机器: 保持原行为(305→300, 481→500)
    printf "%d", int(g/s+0.5)*s }'
}

cmd_probe(){
  need_root
  take_lock
  command -v iperf3 >/dev/null || die "需要 iperf3"
  local peer=""
  while [ $# -gt 0 ]; do
    case "$1" in --peer) peer="$2"; shift 2 ;; *) die "未知参数: $1" ;; esac
  done
  [ -n "$peer" ] || die "需要 --peer <近处的iperf3服务器>"
  set_route_target "$peer"
  local iface; iface=$(detect_iface)
  qdisc_guard "$iface" || { info "已取消"; return 0; }
  info "探测可用带宽（4 并发 + pacing, 约 15 秒）…"
  local bw; bw=$(probe_bandwidth "$peer" "$iface")
  [ -n "$bw" ] || die "探测失败, 检查对端 $peer 是否可达/空闲" 2
  mkdir -p "$STATE_DIR"; echo "BW_MBPS=$bw" > "$STATE_DIR/probe.result"
  ok "估计可用带宽 ≈ ${bw} Mbps"
  echo
  echo "  这只是给 tune 算 BDP 用的估计值, 真正的限速器拐点靠 sweep 实测."
  echo "  下一步: $0 tune --role <proxy|bulk|mixed> --bw $bw"
}

# ── 限速器拐点扫描 ──────────────────────────────────────────────────────────
# 原理: 端口上的限速器(policer)看的是瞬时速率. 不加 pacing 的 TCP 发送是突发的,
# 平均速率没超也会被打穿. 加 fq pacing 后可以贴着真实上限跑而几乎不丢包.
# 拐点 = 重传开始跳变的那一档；取前一档再退安全余量.
# NETTUNE_VERBOSE=1 时把 iperf3 原始输出打到 stderr, 让用户看到测速在跑
# $1=peer $2=dur $3=parallel [$4=port]  -> "sender_mbps retrans [receiver_mbps]"
# 前两列是既有契约, 第三列只追加不改义；没有 receiver 汇总时仍返回两列.
# 公共节点各开十个实例（Leaseweb/OVH 5201-5210, Clouvider 5200-5209）,
# 指定端口忙时自动换 —— 否则单端口一忙就整个失败. 端口表见 PORT_POOL.
run_iperf(){
  local out recv raw tmp port ports pid sg rt rg="" first="${4:-${PEER_PORT:-5201}}"
  ports=$(port_order "$first")
  tmp=$(mktemp)
  for port in $ports; do
    : > "$tmp"
    timeout $TIMEOUT_FG $(( $2 + 25 )) iperf3 $IP_FAMILY -c "$1" -p "$port" -t "$2" -P "$3" -f m >"$tmp" 2>&1 &
    pid=$!
    # --foreground 是必须的: timeout 默认把子进程放进【独立进程组】(方便超时时杀整组),
    # 结果 Ctrl-C 发给脚本进程组的 SIGINT 根本到不了 iperf3, 它会继续满速跑到
    # timeout 到期 —— 9Gbps 的机器上那是十几 GB 白烧. 实测验证过:
    #   默认        iperf3 进程组 ≠ 脚本组, Ctrl-C 后残留 1 个
    #   --foreground 两者相同,        Ctrl-C 后残留 0 个
    # 下面的 trap 是第二道保险, 走 kill 路径时用.
    trap 'kill -TERM "$pid" 2>/dev/null; pkill -P "$pid" 2>/dev/null; rm -f "$tmp"; exit 130' INT TERM HUP
    spin_wait "$pid" "测速中… ${2}s × ${3} 流  →  $1:$port"
    trap - INT TERM HUP
    # 判据是"有没有拿到有效结果", 不是枚举报错文案 —— iperf3 在服务端忙的时候
    # 会随机吐两种错, 早期只认 "busy running a test", 碰上
    # "unable to send control message: Connection reset by peer" 就直接放弃换端口了.
    grep -qE "$( [ "$3" -gt 1 ] && echo 'SUM.*sender' || echo 'sender' )" "$tmp" 2>/dev/null && break
  done
  raw=$(cat "$tmp"); rm -f "$tmp"
  [ "${NETTUNE_VERBOSE:-0}" = 1 ] && echo "$raw" | sed 's/^/      | /' >&2
  out=$(echo "$raw" | grep -E "$( [ "$3" -gt 1 ] && echo 'SUM.*sender' || echo 'sender' )" | tail -1)
  [ -z "$out" ] && { echo ""; return; }
  recv=$(echo "$raw" | grep -E "$( [ "$3" -gt 1 ] && echo 'SUM.*receiver' || echo 'receiver' )" | tail -1)
  sg=$(echo "$out"  | awk '{print $(NF-3)}')
  rt=$(echo "$out"  | awk '{print $(NF-1)}')
  [ -n "$recv" ] && rg=$(echo "$recv" | awk '{print $(NF-2)}')
  if [ -n "$rg" ]; then printf '%s %s %s\n' "$sg" "$rt" "$rg"
  else                     printf '%s %s\n'    "$sg" "$rt"; fi
}

# 丢包率(%) = 重传数 / 发出的包数. 包数按 1448 字节 MSS 估算.
#
# 为什么不能用绝对次数：阈值 100 在 300M 机上相当于 0.032% 丢包,
# 在 7.4G 机上只有 0.0014% —— 严了 25 倍. 实测踩过：一台 10G 口的机器
# 第一档 7440Mbit 实测 7001Mbps、重传 101（丢包率 0.0014%, 链路干净得离谱）,
# 却被判成撞了限速器, LAST_OK 为空直接报 "no usable rate measured" 退出,
# 整个扫描一档都没跑成.
#
# 七组真实数据回归：干净侧最高 0.0017%, 撞限速器最低 1.3541%.
# 阈值取 0.1%, 距两侧分别有 59 倍和 13.5 倍余量, 且自动适配任何带宽.
loss_pct(){   # loss_pct <重传数> <吞吐Mbps> <秒数>
  awk -v rt="$1" -v gp="$2" -v d="$3" 'BEGIN{
    pk = gp*1000000*d/8/1448          # 发出的包数
    if(pk < 1) pk = 1
    printf "%.4f", rt*100/pk
  }'
}

cmd_sweep(){
  need_root
  take_lock
  command -v iperf3 >/dev/null || die "需要 iperf3: apt install -y iperf3 / yum install -y iperf3"
  # GAP: 档与档之间的静置时间, 让上一条流的状态排空, 避免相邻两档互相干扰
  local peer="" nominal="" lo="" hi="" step="" dur=12 par=1 margin="" thresh=0.1 refine=1 GAP=3 cap=10000
  local PRE_SCAN_GAP=15 BASELINE_CAP=0.5
  # cap 和 AGG_MIN 是两件事, 早期版本共用一个值, 抬 cap 会连带改掉"多大算大机器":
  #   cap     —— 愿意扫到多高（--cap 可调）
  #   AGG_MIN —— 单流不可信、要用 8 流复核的带宽门槛（跟 cap 无关）
  local AGG_MIN=2500
  local assume_yes=0 agreed_gb=""
  while [ $# -gt 0 ]; do
    case "$1" in
      --peer) peer="$2"; shift 2 ;;
      --yes|-y) assume_yes=1; shift ;;   # 跳过流量确认, 给无人值守用
      # 向导内部用: 用户在确认页上已经看过并同意的预估流量(GB)
      --agreed-gb) agreed_gb="$2"; shift 2 ;;
      --port) PEER_PORT="$2"; shift 2 ;;
      -4) IP_FAMILY="-4"; shift ;;
      -6) IP_FAMILY="-6"; shift ;;
      --nominal) nominal="$2"; shift 2 ;;
      # 前导零要去掉: is_posint 认为 "08" 合法, 后面的 bash 算术却按八进制
      # 解析并报 "value too great for base", 实际跳过预期起点.
      --from) lo=$(strip_zeros "$2"); shift 2 ;;
      --to) hi=$(strip_zeros "$2"); shift 2 ;;
      --step) step=$(strip_zeros "$2"); shift 2 ;;
      --dur) dur="$2"; shift 2 ;;
      --parallel) par="$2"; shift 2 ;;
      --margin) margin="$2"; shift 2 ;;   # 校验在下面统一做
      --gap) GAP="$2"; shift 2 ;;
      --cap) cap="$2"; shift 2 ;;
      --no-refine) refine=0; shift ;;
      --loss-threshold|--retrans-threshold) thresh="$2"; shift 2 ;;   # 单位是百分比
      *) die "未知参数: $1" ;;
    esac
  done
  # margin 必须一起校验: 负值会让 RECOMMEND = KNEE - margin 高于实测拐点,
  # 等于建议用户把限速设到限速器之上, 整形完全失效. 实测 --margin -100
  # 会写出 KNEE=20 / RECOMMEND=120.
  for _v in "margin:$margin:0:1000000" \
            "nominal:$nominal:1:1000000" "step:$step:1:100000" "dur:$dur:1:600" \
            "par:$par:1:128" "lo:$lo:1:1000000" "hi:$hi:1:1000000" "gap:$GAP:0:60"; do
    _n=${_v%%:*}; _r=${_v#*:}; _val=${_r%%:*}; _r=${_r#*:}; _min=${_r%%:*}; _max=${_r#*:}
    [ -z "$_val" ] && continue
    is_posint "$_val" "$_min" "$_max" || die "--${_n} 必须是 ${_min}-${_max} 的整数"
  done
  # 向导内部参数, 不能因为它让向导半路退出: 逗号小数点(mawk + 德俄等 locale)换成点,
  # 仍认不出就当没给 —— 退回菜单 3 / CLI 的规则(预计超过确认线就问), 依然安全.
  agreed_gb=${agreed_gb/,/.}
  if [ -n "$agreed_gb" ] && ! [[ $agreed_gb =~ ^[0-9]+(\.[0-9]+)?$ ]]; then
    warn "忽略无法识别的 --agreed-gb: $agreed_gb"; agreed_gb=""
  fi
  [ -n "$peer" ] || die "需要 --peer <iperf3服务器>, 选延迟低的, 测的是本机端口上限而非跨国链路"
  set_route_target "$peer"
  # 用户填的带宽要单独留一份: 后面 nominal 可能被实测值覆盖,
  # 而"实测远超所填"的判断必须拿用户原话去比.
  local stated_nominal="$nominal"
  local iface; iface=$(detect_iface)
  # 手工给了区间就完全按用户说的来, 不做不限速探测
  local user_range=""; [ -n "$lo" ] && [ -n "$hi" ] && user_range=1
  # 手工给了区间: 缺的标称值用区间上界顶上. 自动模式下 nominal/lo/hi/step
  # 全部由后面的不限速探测决定, 这里不需要它们.
  if [ -n "$user_range" ]; then
    [ -n "$nominal" ] || nominal="$hi"
    [ -n "$step" ] || step=$(calc_step "$nominal")
  fi
  is_posint "$cap" 100 100000 || die "--cap 必须是 100-100000 的整数"
  # 校验一过就清掉上一轮的结果, 必须在任何 return 之前 ——
  # 否则这轮失败(对端太慢/探测失败/取消)时, 向导和菜单会读到上次的 RECOMMEND
  # 并把旧限速值应用上去, 而屏幕上写的是 "shaping skipped".
  mkdir -p "$STATE_DIR"; rm -f "$STATE_DIR/sweep.result"
  qdisc_guard "$iface" || { info "已取消"; return 0; }

  [ "$WIZARD" = 1 ] || traffic_mark
  info "Peer ${peer}:${PEER_PORT}"

  # 扫描会反复替换 qdisc；无论正常结束、拐点 break 还是被 Ctrl-C,
  # 都必须把机器恢复原状 —— 否则会被留在那个暴丢包的档位上.
  qdisc_save "$iface"
  restore_qdisc(){ qdisc_restore; info "qdisc restored"; }
  trap 'echo; warn "interrupted, restoring qdisc..."; qdisc_restore; exit 130' INT TERM HUP   # 中断退出是对的

  # 扫一段区间. 结果放进全局 LAST_OK(最后一个干净档) 与 BROKE_AT(重传跳变的那档)
  LAST_OK=""; BROKE_AT=""; SLOW_HITS=0; PEER_TOO_SLOW=0; BASE_LOSS=""; SPIKE_MIN_LOSS=""; SLOW_AT=""
  # 扫描覆盖率. 只有"真的测过并拿到有效样本"的档才算验证过 ——
  # 早期版本把区间上界 hi 直接当成"已扫到", 于是上界那一档下发整形失败、
  # 或对端连续三次没结果被跳过时, 仍然输出"扫到 40 Mbit 未检测到限速器",
  # 向导据此把用户的旧整形删掉. 覆盖不足必须报"判不出"而不是"没有".
  SCAN_VERIFIED=0        # 拿到有效样本的档数
  SCAN_MAX_OK=""         # 实际验证到的最高速率
  SCAN_INCOMPLETE=0      # 有档位失败或被跳过
  # 跳变判定: 既要超过绝对阈值, 也要明显高于本底. 远程对端可能有
  # 0.1%-0.3% 的稳定底噪, 所以用 5 倍本底; 同时把相对阈值封顶在 1%,
  # 避免底噪把实测 1.35% 以上的 policer 拐点完全遮住.
  is_spike(){
    awk -v l="$1" -v t="$thresh" -v b="${BASE_LOSS:-0}" 'BEGIN{
      need=t
      if (b > 0 && b*5 > need) need=b*5
      if (need > 1) need=1
      if (l <= need) exit 1
      exit 0
    }' 2>/dev/null
  }
  scan_range(){
    local a b st r res sgp gp rt lp prev_gp=0 verdict
    a=$1; b=$2; st=$3
    # 终点必测: 168→221 步长 20 只会测 168/188/208, 而提示里写的是"扫到 221",
    # 上界从来没被验证过. 补一档把终点带上.
    local pts="" _r
    for (( _r=a; _r<=b; _r+=st )); do pts="$pts $_r"; done
    case " $pts " in *" $b "*) ;; *) pts="$pts $b" ;; esac
    for r in $pts; do
      if ! apply_test_shaper "$iface" "$r"; then
        warn "failed to apply test shaper at ${r} Mbit"
        SCAN_INCOMPLETE=1; return 1
      fi
      res=""
      # 进度提示交给 run_iperf 里的转圈, 这里不要再打占位符（会和转圈重叠）
      for _ in 1 2 3; do res=$(run_iperf "$peer" "$dur" "$par"); [ -n "$res" ] && break; sleep 8; done
      if [ -z "$res" ]; then
        printf '  %-10s %12s %9s %8s  %s\n' "$r" "-" "-" "-" "peer busy, skipped"
        SCAN_INCOMPLETE=1; continue
      fi
      sgp=$(echo "$res" | awk '{print $1}'); rt=$(echo "$res" | awk '{print $2}')
      gp=$(echo "$res" | awk '{print $3}'); [ -n "$gp" ] || gp="$sgp"
      lp=$(loss_pct "$rt" "$sgp" "$dur")
      SCAN_VERIFIED=$(( SCAN_VERIFIED + 1 )); SCAN_MAX_OK="$r"
      verdict="ok"
      # 只有干净样本才能建立本底. 不限速探测刚打穿 policer 时,
      # 第一档可能带 0.4%-8% 的假丢包; 把它当基线后再要求 10 倍跳变,
      # 真实拐点就永远触发不了. 首个超阈值样本必须按疑似跳变复测.
      if [ -z "$BASE_LOSS" ] && awk -v l="$lp" -v t="$thresh" 'BEGIN{exit !(l <= t)}'; then
        BASE_LOSS="$lp"
      fi
      # 判定用丢包率而非绝对重传数 —— 见 loss_pct 上方注释.
      # 单次跳变可能只是公共节点被别人占用, 所以要复测: 3 次里 ≥2 次跳变才确认.
      if is_spike "$lp"; then
        local hits=1 j clean_gp="" clean_rt="" clean_lp=""
        local min_lp="$lp"
        for j in 2 3; do
          sleep "$GAP"
          local r2 s2 g2 t2 l2
          r2=$(run_iperf "$peer" "$dur" "$par"); [ -z "$r2" ] && continue
          s2=$(echo "$r2" | awk '{print $1}'); t2=$(echo "$r2" | awk '{print $2}')
          g2=$(echo "$r2" | awk '{print $3}'); [ -n "$g2" ] || g2="$s2"
          l2=$(loss_pct "$t2" "$s2" "$dur")
          printf '  %-10s %12s %9s %8s  %s\n' "${r} (#${j})" "$g2" "$t2" "$l2" "recheck"
          if awk -v x="$l2" -v y="$min_lp" 'BEGIN{exit !(x < y)}'; then
            min_lp="$l2"
          fi
          if is_spike "$l2"; then
            hits=$(( hits + 1 ))
          else
            clean_gp="$g2"; clean_rt="$t2"; clean_lp="$l2"
            [ -n "$BASE_LOSS" ] || BASE_LOSS="$l2"
          fi
        done
        if [ "$hits" -ge 2 ]; then
          # 首档 0.18% 也可能就是真实 policer 拐点，不能仅凭它低于 0.5%
          # 就收作线路底噪。把最低值留给主流程，必要时向下测控制点再判断.
          SPIKE_MIN_LOSS="$min_lp"
          printf '  %-10s %12s %9s %8s  %s\n' "$r" "$gp" "$rt" "$lp" "$(_c '0;31' "loss spike (${hits}/3)")"
          BROKE_AT=$r; return 0
        fi
        if [ -n "$clean_gp" ]; then
          gp="$clean_gp"; rt="$clean_rt"; lp="$clean_lp"
          [ "$verdict" = ok ] && verdict="transient (1/3), clean recheck used"
        else
          printf '  %-10s %12s %9s %8s  %s\n' "$r" "$gp" "$rt" "$lp" "$(_c '0;33' "transient (1/3), no clean recheck")"
          sleep "$GAP"; continue
        fi
      fi
      # 吞吐远低于限速值、重传却很低 = 整形器压根没被触发, 瓶颈在对端.
      # 「重传低」这个条件必不可少：吞吐低但重传高是真撞限速器, 那是有效数据.
      if awk -v g="$gp" -v r="$r" -v l="$lp" -v t="$thresh" \
         'BEGIN{exit !(g < r*0.7 && l <= t)}' 2>/dev/null; then
        SLOW_HITS=$(( SLOW_HITS + 1 ))
        printf '  %-10s %12s %9s %8s  %s\n' "$r" "$gp" "$rt" "$lp" \
          "$(_c '0;33' "only $(awk -v g="$gp" -v r="$r" 'BEGIN{printf "%d", g*100/r}')% of target")"
        [ "$SLOW_HITS" -ge 3 ] && { PEER_TOO_SLOW=1; SLOW_AT="$r"; return 0; }
        LAST_OK=$r; prev_gp=$gp; sleep "$GAP"; continue
      fi
      SLOW_HITS=0
      # 吞吐不再增长也说明到顶了
      if [ "$verdict" = ok ] && awk -v x="$gp" -v y="$prev_gp" 'BEGIN{exit !(y>0 && x < y*1.01)}'; then verdict="no further gain"; fi
      printf '  %-10s %12s %9s %8s  %s\n' "$r" "$gp" "$rt" "$lp" "$verdict"
      LAST_OK=$r; prev_gp=$gp
      sleep "$GAP"
    done
  }

  # ── 不限速探测 ──────────────────────────────────────────────────────────
  # 直接放开跑一次: 丢包低 = 没东西在打你 = 不用整形; 丢包高 = 有限速器, 再去找它.
  #
  # 关键: 拐点在【不限速吞吐之上】, 不是之下. 打穿限速器会让吞吐掉下来 ——
  # LA 机不限速 481 Mbps / 丢包 5.70%, 而真实拐点在 530(限到 530 反而跑 499);
  # 美国机不限速 1262 / 3.44%, 拐点 1340. 从不限速吞吐往下找会直接错过.
  #
  # 用单流: 多流的丢包归因不干净, 而且这个项目面向国内优化线路, 单流是实际场景.
  local ug="" ug_recv="" ulp="" cap_gp="" cap_streams=1 single_printed=0
  if [ -z "$user_range" ]; then
    info "Unshaped probe (no rate limit, ${dur}s, 1 stream)"
    printf '  %-10s %12s %9s %8s  %s\n' "Rate/Mbit" "Goodput/Mbps" "Retrans" "Loss%" "Verdict"
    qdisc_set_fq "$iface" || { qdisc_restore; warn "failed to enable fq for unshaped probe"; return 2; }
    local ures urt
    for _ in 1 2 3; do ures=$(run_iperf "$peer" "$dur" 1); [ -n "$ures" ] && break; sleep 8; done
    [ -n "$ures" ] || { qdisc_restore; warn "unshaped probe failed, check the peer"; return 2; }
    ug=$(echo "$ures" | awk '{print $1}'); urt=$(echo "$ures" | awk '{print $2}')
    ug_recv=$(echo "$ures" | awk '{print $3}')
    ulp=$(loss_pct "$urt" "$ug" "$dur")
    cap_gp="${ug_recv:-$ug}"

    # 自动带宽探测用 4 流, 这里用单流找 policer. 公共节点偶发拥塞时, 单次
    # receiver 可能只剩自动探测值的一小部分, 直接拿它推扫描区间会把 40M
    # 机器误扫成 14M、最终持久限到 12M. 只在低于 70% 时补两次, 正常机器
    # 不增加时长；三次取 receiver 最高的【整组】结果, sender/重传必须同步换.
    # 大带宽机（>=AGG_MIN）沿用后面的 8 流保护, 不在这里重复增加两轮单流.
    if [ -n "$nominal" ] && [ "$nominal" -lt "$AGG_MIN" ] 2>/dev/null && awk -v g="$cap_gp" -v n="$nominal" \
       'BEGIN{exit !(g < n*0.7)}'; then
      local best_res="$ures" best_gp="$cap_gp" samples=1 sample_n extra
      local es er egp ert elp
      info "Single stream reached ${cap_gp} Mbps (<70% of ${nominal}); taking 2 more samples"
      printf '  %-10s %12s %9s %8s  %s\n' "none (#1)" "$cap_gp" "$urt" "$ulp" "sample"
      for sample_n in 2 3; do
        sleep "$GAP"
        extra=""
        for _ in 1 2 3; do extra=$(run_iperf "$peer" "$dur" 1); [ -n "$extra" ] && break; sleep 8; done
        if [ -z "$extra" ]; then
          printf '  %-10s %12s %9s %8s  %s\n' "none (#${sample_n})" "-" "-" "-" "peer busy, skipped"
          continue
        fi
        samples=$(( samples + 1 ))
        es=$(echo "$extra" | awk '{print $1}'); ert=$(echo "$extra" | awk '{print $2}')
        er=$(echo "$extra" | awk '{print $3}'); egp="${er:-$es}"
        elp=$(loss_pct "$ert" "$es" "$dur")
        printf '  %-10s %12s %9s %8s  %s\n' "none (#${sample_n})" "$egp" "$ert" "$elp" "sample"
        if awk -v x="$egp" -v y="$best_gp" 'BEGIN{exit !(x > y)}'; then
          best_res="$extra"; best_gp="$egp"
        fi
      done
      ures="$best_res"
      ug=$(echo "$ures" | awk '{print $1}'); urt=$(echo "$ures" | awk '{print $2}')
      ug_recv=$(echo "$ures" | awk '{print $3}')
      ulp=$(loss_pct "$urt" "$ug" "$dur")
      cap_gp="${ug_recv:-$ug}"
      info "Using best of ${samples}: ${cap_gp} Mbps"
    fi
    qdisc_restore

    # 大带宽机不能只凭单流决定是否进入扫描. 长 RTT / 对端接收窗口会把 10G
    # 机器的单流压到 2.5G 以下; 如果这条单流又恰好有路径丢包, 旧逻辑会把
    # 它误认成低速 policer 并扫描几百兆区间. 只在用户标称值已达到 AGG_MIN、
    # 且单流结果确实可疑时补一次 8 流确认，不给普通低带宽扫描增加流量.
    # 没填带宽(菜单 3 回车 / CLI 不带 --nominal)时不知道机器多大, 单流可疑也要复核:
    # 早期这里直接按单流去扫, 10G 无限速机器得出"建议整形 1464", 菜单 3 回车就限上了.
    # 多花的流量受端口速率封顶, 小机器上只是一档 8 流.
    if { [ -z "$nominal" ] || [ "$nominal" -ge "$AGG_MIN" ] 2>/dev/null; } && \
       awk -v g="$cap_gp" -v c="$cap" -v l="$ulp" -v t="$thresh" \
         'BEGIN{exit !(g <= c && l > t)}'; then
      local ares="" ag art ar alp
      if [ -n "$nominal" ]; then
        info "Single stream is inconclusive on a ${nominal} Mbit host; checking 8-stream aggregate"
      else
        info "Single stream is inconclusive and no bandwidth was given; checking 8-stream aggregate"
      fi
      qdisc_set_fq "$iface" || { qdisc_restore; warn "failed to enable fq for aggregate probe"; return 2; }
      for _ in 1 2 3; do ares=$(run_iperf "$peer" "$dur" 8); [ -n "$ares" ] && break; sleep 8; done
      qdisc_restore
      if [ -n "$ares" ]; then
        ag=$(echo "$ares" | awk '{print $1}'); art=$(echo "$ares" | awk '{print $2}')
        ar=$(echo "$ares" | awk '{print $3}'); [ -n "$ar" ] || ar="$ag"
        alp=$(loss_pct "$art" "$ag" "$dur")
        if awk -v g="$ar" -v c="$cap" 'BEGIN{exit !(g > c)}'; then
          printf '  %-10s %12s %9s %8s  %s\n' "none (1x)" "$cap_gp" "$urt" "$ulp" "inconclusive"
          printf '  %-10s %12s %9s %8s  %s\n' "none (8x)" "$ar" "$art" "$alp" "above cap"
          cap_gp="$ar"; cap_streams=8
        else
          printf '  %-10s %12s %9s %8s  %s\n' "none (1x)" "$cap_gp" "$urt" "$ulp" "$(_c '0;31' 'loss -- possible policer')"
          printf '  %-10s %12s %9s %8s  %s\n' "none (8x)" "$ar" "$art" "$alp" "aggregate below cap"
          # 8 流也干净 = 这个聚合速率上没有限速器在打, 单流的丢包是单条连接自己的
          # (对端 / 路径). 不能再按单流去扫: 模型实测 8 流 9600 零丢包, 按单流 1950
          # 扫出 1916, 等于把一台没有限速器的 10G 机器限到五分之一.
          # 0.5.7 把上限从 2500 提到 10000 之后, 10G 机器的聚合多半低于上限, 才走得到这里.
          if ! awk -v l="$alp" -v t="$thresh" 'BEGIN{exit !(l > t)}'; then
            echo
            mkdir -p "$STATE_DIR"
            # 8 流干净, 但跑得不够高时证明不了什么(可能就是对端只给这么多):
            #   有标称 → 不到标称 70% 判不出
            #   没标称 → 没高过单流 1.5 倍判不出(高出很多才说明单流的丢包是单条连接自己的)
            local _weak=""
            if [ -n "$nominal" ]; then
              if awk -v g="$ar" -v n="$nominal" 'BEGIN{exit !(g < n*0.7)}' 2>/dev/null; then
                _weak="不到标称 ${nominal} 的 70%"
              fi
            elif awk -v g="$ar" -v s="$cap_gp" 'BEGIN{exit !(g <= s*1.5)}'; then
              _weak="和单流 ${cap_gp} 差不多"
            fi
            if [ -n "$_weak" ]; then
              warn "8 流只送达 ${ar} Mbps, ${_weak} —— 判不出有没有限速器."
              echo "  多半是对端太慢或路径拥塞. 换个更近/更空闲的对端重测."
              [ -n "$nominal" ] || echo "  填上套餐带宽(--nominal / 菜单里填数字)能判得更准."
              [ -x "$QDISC_SCRIPT" ] && echo "  已有整形保持不动."
              {
                printf 'INCONCLUSIVE=1\nUNSHAPED=%s\n' "$ar"
                if [ -n "$nominal" ]; then printf 'NOMINAL=%s\n' "$nominal"; fi
              } > "$STATE_DIR/sweep.result"
            else
              warn "不限速 8 流送达 ${ar} Mbps, 丢包 ${alp}%, 未检测到限速器."
              echo "  单流的丢包来自单条连接本身(对端或路径), 不是限速器."
              # 没填带宽时只能证明 ${ar} 以内没有限速器: 对端把 8 流卡在限速器下面也是这个结果
              [ -n "$nominal" ] || echo "  没填带宽, 只能说明 ${ar} Mbps 以内没有限速器, 更高处判不出."
              printf 'NO_KNEE=1\nUNSHAPED=%s\n' "$ar" > "$STATE_DIR/sweep.result"
            fi
            trap - INT TERM HUP     # qdisc 上面已经恢复过了, 别让 Ctrl-C 处理器留到菜单里
            traffic_report
            return 3
          fi
        fi
        single_printed=1
      fi
    fi

    if awk -v g="$cap_gp" -v c="$cap" 'BEGIN{exit !(g > c)}'; then
      [ "$single_printed" = 1 ] || \
        printf '  %-10s %12s %9s %8s  %s\n' "none" "$cap_gp" "$urt" "$ulp" "above cap"
      echo
      warn "不限速 ${cap_streams} 流能送达 ${cap_gp} Mbps, 超过 ${cap} Mbit 的扫描上限."
      echo "  这个量级基本只有专线和内网, 限速器很少见."
      echo "  确定要扫的话: tcpfit sweep --peer <对端> --cap <更大的值>"
      mkdir -p "$STATE_DIR"; printf 'NO_KNEE=1\nABOVE_CAP=%s\nUNSHAPED=%s\n' "$cap" "$cap_gp" > "$STATE_DIR/sweep.result"
      traffic_report
      return 3
    fi

    if ! awk -v l="$ulp" -v t="$thresh" 'BEGIN{exit !(l > t)}'; then
      # 干净样本只能证明"在【测到的这个速率上】没触发限速器".
      # 连标称的 70% 都没跑到时, 它证明不了标称速率附近没有限速器 ——
      # 早期版本一律写 NO_KNEE=1, 向导据此把用户实测得来的旧整形删掉.
      # 事故形态: 对端拥塞 -> 95 Mbps 干净 -> 删掉 950 Mbit 的 HTB.
      # 这里必须和"确信没有限速器"分开表达, 让向导保留已有配置.
      if [ -n "$nominal" ] && awk -v g="$cap_gp" -v n="$nominal" \
         'BEGIN{exit !(n > 0 && g < n*0.7)}' 2>/dev/null; then
        printf '  %-10s %12s %9s %8s  %s\n' "none" "$cap_gp" "$urt" "$ulp" \
          "$(_c '0;33' 'inconclusive')"
        echo
        warn "只送达 ${cap_gp} Mbps, 不到标称 ${nominal} 的 70% —— 判不出有没有限速器."
        echo "  干净样本只说明这个速率上没撞限速器, 不代表 ${nominal} 附近没有."
        echo "  多半是对端太慢或路径拥塞. 换个更近/更空闲的对端重测."
        [ -x "$QDISC_SCRIPT" ] && echo "  已有整形保持不动."
        mkdir -p "$STATE_DIR"
        printf 'INCONCLUSIVE=1\nUNSHAPED=%s\nNOMINAL=%s\n' "$cap_gp" "$nominal" \
          > "$STATE_DIR/sweep.result"
        traffic_report
        return 3
      fi
      printf '  %-10s %12s %9s %8s  %s\n' "none" "$cap_gp" "$urt" "$ulp" "ok"
      echo
      warn "不限速送达 ${cap_gp} Mbps, 丢包 ${ulp}%, 未检测到限速器."
      mkdir -p "$STATE_DIR"; printf 'NO_KNEE=1\nUNSHAPED=%s\n' "$cap_gp" > "$STATE_DIR/sweep.result"
      traffic_report
      return 3
    fi

    [ "$single_printed" = 1 ] || \
      printf '  %-10s %12s %9s %8s  %s\n' "none" "$cap_gp" "$urt" "$ulp" "$(_c '0;31' 'loss -- policer present')"
    # ug 是 iperf3【发送端】的数字, 它包含了"写进 socket 但没送达"的部分 ——
    # 干净链路上和接收端只差 3%, 但丢包链路上差很多. 实测一台香港 CN2:
    # 不限速 发送 18.3 / 接收 14.6（丢包 23.6%）, 而干净区上限只有 15.
    # 直接拿 18.3 推区间会得到 17→31, 起点就已经在丢包区里, 整个扫描跑偏.
    # receiver 是 iperf3 实测送达量, 比按重传率反推更准确. 极老版本没有
    # receiver 汇总时第三列为空, 保留原公式作为兼容兜底.
    local ug_eff="$ug_recv"
    [ -n "$ug_eff" ] || ug_eff=$(awk -v g="$ug" -v l="$ulp" 'BEGIN{
      v=g*(1-l/100); if(v<1)v=1; printf "%.1f", v }')
    # 拐点在（真实送达量）之上, 所以区间从它稍下方起, 往上扫.
    # 打穿限速器后 goodput 会掉下来, 丢得越狠掉得越多, 所以上界要按丢包率放宽:
    # 24% 丢包时真实拐点可能比 1.25×goodput 高得多(群里碰到过 177Mbps/24% 的例子).
    lo=$(awk -v g="$ug_eff" 'BEGIN{v=int(g*0.95); if(v<1)v=1; printf "%d", v}')
    hi=$(awk -v g="$ug_eff" -v c="$cap" -v l="$ulp" 'BEGIN{
      k = 1.25 + l/100*2          # 丢包越高, 真实拐点离 goodput 越远
      if (k > 2.5) k = 2.5
      v = g*k; if (v > c) v = c
      printf "%d", v }')
    [ "$hi" -gt "$lo" ] 2>/dev/null || hi=$(( lo + 2 ))
    [ -n "$nominal" ] || nominal=$(awk -v g="$ug_eff" 'BEGIN{printf "%d", g}')
    # 步长必须按【区间宽度】推, 不能按 nominal 推 —— calc_step 对 ≤600M 恒等于 20,
    # 而小带宽机器的区间可能只有十几宽, 20 的步长只能采到 1-2 个点, 定不出拐点.
    # 实测香港 CN2: 区间 17→31（宽 14）, step 20 → 只测了 17 和 31 两档.
    # 目标是区间内约 10 个采样点; 大机器上这个公式给出的值和 calc_step 基本一致.
    [ -n "$step" ] || step=$(awk -v lo="$lo" -v hi="$hi" 'BEGIN{
      s = int((hi-lo)/10 + 0.5); if (s < 1) s = 1; printf "%d", s }')
    # 向导开始前的流量预估是按【用户填的带宽】算的 —— 填 200 就告诉他约 3 GB.
    # 可一台 10G 口的机器不限速能跑 8532, 往下扫的流量是按实测速率走的.
    # 客户实报: 同意的是 3 GB, 实际跑掉 400 GB. 用户同意的数和实际花的数必须对得上.
    # 无 tty 时 confirm 走默认 n, 直接中止 —— 宁可不扫, 也不能无人值守地烧几百 GB.
    # 三种情况要停下来问:
    #   over   —— 实测远超所填(3 倍以上): 用户同意的流量是按所填带宽估的
    #   agreed —— 向导里用户已经看过一个预估, 实际要跑的远超它(1.5 倍以上, 且超过确认线)
    #   big    —— 菜单 3 / 命令行: 开始前没给过任何预估, 超过确认线就问
    # 早期只有 over: 没填带宽时没东西可比, 一声不响地扫下去; 填 3000、实测 8100
    # (不到 3 倍)的 10G 机器也是一句不问, 扫掉约 200 GB.
    local _pts _gb _why=""
    _pts=$(( (hi - lo) / step + 2 + 8 ))          # 粗扫各档 + 终点 + 精修上限
    _gb=$(awk -v p="$_pts" -v a="$lo" -v b="$hi" -v d="$dur" \
          'BEGIN{printf "%.0f", p*(a+b)/2*d/8/1024}')
    if [ -n "$stated_nominal" ] &&
       awk -v g="$ug_eff" -v n="$stated_nominal" 'BEGIN{exit !(n > 0 && g > n*3)}' 2>/dev/null; then
      _why=over
    elif [ -n "$agreed_gb" ]; then
      if awk -v g="$_gb" -v a="$agreed_gb" -v t="$TRAFFIC_CONFIRM_GB" \
           'BEGIN{exit !(g > a*1.5 && g > t)}'; then
        _why=agreed
      fi
    elif awk -v g="$_gb" -v t="$TRAFFIC_CONFIRM_GB" 'BEGIN{exit !(g > t)}'; then
      _why=big
    fi
    if [ -n "$_why" ] && [ "$assume_yes" != 1 ]; then
      echo
      if [ "$_why" = over ]; then warn "不限速实测送达 ${ug_eff} Mbps,远高于写入带宽"
      else                        warn "不限速实测送达 ${ug_eff} Mbps"; fi
      echo "    按实测速率, 接下来的扫描可能高达约 ${_gb} GB"
      # 不说"按所填带宽": 带宽回车时预估是按实测值算的, 用户什么都没填
      if [ "$_why" = agreed ]; then echo "    开始前给你看的预估是约 ${agreed_gb} GB"; fi
      if ! confirm "  按实测速率继续扫描？" n; then
        trap - INT TERM HUP
        restore_qdisc
        info "已取消扫描, 没有下发任何测试整形; 已有整形保持不动"
        have_tty || echo "    没有终端时默认取消; 无人值守请加 --yes"
        # 取消要和「测失败」分开: 早期版本这里 return 2, 向导当成扫描失败,
        # 接着照样满速跑验证(10G 机器又是 20 GB), 结果页也说不出原因.
        mkdir -p "$STATE_DIR"
        printf 'CANCELLED=1\nUNSHAPED=%s\n' "$ug_eff" > "$STATE_DIR/sweep.result"
        traffic_report
        return 3
      fi
    fi
    info "Policer present, scanning ${lo} -> ${hi} Mbit（不限速实测送达 ${ug_eff} Mbps）"
    info "Cooling down ${PRE_SCAN_GAP}s before the first scan point"
    sleep "$PRE_SCAN_GAP"
  fi

  # 手工区间(--from/--to)不做不限速探测, 上面那道确认走不到. 而 OUT_OF_RANGE 时工具自己
  # 建议的下一条命令就是 sweep --from X --to 2X —— 10G 口上一次 200 GB 也不问(复审实测).
  # 按区间速率估(实际速率被端口封顶, 这是上限), 规则同菜单 3 / CLI: 超过确认线先问.
  if [ -n "$user_range" ] && [ "$assume_yes" != 1 ]; then
    local _mpts _mgb
    _mpts=$(( (hi - lo) / step + 2 + 8 ))
    _mgb=$(awk -v p="$_mpts" -v a="$lo" -v b="$hi" -v d="$dur" \
           'BEGIN{printf "%.0f", p*(a+b)/2*d/8/1024}')
    if awk -v g="$_mgb" -v t="$TRAFFIC_CONFIRM_GB" 'BEGIN{exit !(g > t)}'; then
      echo
      warn "手工区间 ${lo} -> ${hi} Mbit, 按这个区间估算, 扫描可能高达约 ${_mgb} GB"
      if ! confirm "  按这个区间继续扫描？" n; then
        trap - INT TERM HUP
        restore_qdisc
        info "已取消扫描, 没有下发任何测试整形; 已有整形保持不动"
        have_tty || echo "    没有终端时默认取消; 无人值守请加 --yes"
        mkdir -p "$STATE_DIR"; printf 'CANCELLED=1\n' > "$STATE_DIR/sweep.result"
        traffic_report
        return 3
      fi
    fi
  fi

  echo
  info "Scanning ${lo} -> ${hi} Mbit, step ${step}, ${dur}s each, threshold loss > ${thresh}%"
  printf '  %-10s %12s %9s %8s  %s\n' "Rate/Mbit" "Goodput/Mbps" "Retrans" "Loss%" "Verdict"
  scan_range "$lo" "$hi" "$step" || SCAN_INCOMPLETE=1

  # 自动区间第一档就连续丢包时，起点可能已越过一个很浅的 policer 拐点；
  # 也可能只是远端路径稳定的 0.1%-0.3% 底噪。向下 25% 测控制点来区分：
  #   低速档干净        -> 首档是真拐点，保留这个上下界供细扫；
  #   两档损失相近且<=0.5% -> 才确认是线路底噪，再从原区间继续扫.
  # 最多向下三次，宁可不给整形值，也不把仍在丢包区的值推荐给用户.
  if [ -z "$user_range" ] && [ -z "$LAST_OK" ] && [ -n "$BROKE_AT" ]; then
    local known_broke="$BROKE_AT" known_loss="$SPIKE_MIN_LOSS"
    local control control_loss attempts=0
    while [ "$attempts" -lt 3 ] && [ -z "$LAST_OK" ]; do
      attempts=$(( attempts + 1 ))
      control=$(( known_broke * 3 / 4 ))
      [ "$control" -lt 1 ] && control=1
      [ "$control" -lt "$known_broke" ] || break
      info "First scan point is lossy; checking ${control} Mbit as a lower-rate control"
      LAST_OK=""; BROKE_AT=""; BASE_LOSS=""; SPIKE_MIN_LOSS=""
      scan_range "$control" "$control" 1

      if [ -n "$LAST_OK" ]; then
        BROKE_AT="$known_broke"
        break
      fi
      control_loss="$SPIKE_MIN_LOSS"
      if [ -n "$control_loss" ] && [ -n "$known_loss" ] && \
         awk -v a="$control_loss" -v b="$known_loss" -v c="$BASELINE_CAP" 'BEGIN{
           d=a-b; if(d<0)d=-d
           exit !(a<=c && b<=c && d<=0.1)
         }'; then
        BASE_LOSS=$(awk -v a="$control_loss" -v b="$known_loss" 'BEGIN{print (a<b?a:b)}')
        LAST_OK="$control"
        BROKE_AT=""
        info "Stable path loss confirmed at ${BASE_LOSS}%; continuing with a lower-rate baseline"
        scan_range "$known_broke" "$hi" "$step"
        break
      fi
      [ -n "$control_loss" ] || break
      known_broke="$control"
      known_loss="$control_loss"
    done
  fi

  if [ "$PEER_TOO_SLOW" = 1 ]; then
    echo
    trap - INT TERM HUP
    restore_qdisc
    [ "$WIZARD" = 1 ] && printf '\n  %s════ 结果 ══════════════════════════════════════════════%s\n' "$bold" "$plain"
    echo
    # 连续三档达不到限速值, 原因有两种, 早期版本一律报"对端太慢".
    # 扫描上限还是 2500 时这个错标很少露面; 抬到 10000 之后大机器会经常走到这里,
    # 被指使去换对端, 换完还是一样 —— 因为瓶颈根本在本机.
    # 判据: 掉速发生在【不限速实测吞吐】之上, 说明这速率本机本来就跑不到.
    # 实测依据: 1 核机器在 3000 Mbit 档 HTB 只能送达 71.5%, 而判据线是 70%.
    if [ -n "$cap_gp" ] && [ -n "$SLOW_AT" ] && \
       awk -v s="$SLOW_AT" -v g="$cap_gp" 'BEGIN{exit !(g > 0 && s > g*1.1)}' 2>/dev/null; then
      warn "扫到 ${SLOW_AT} Mbit 时连续达不到目标 —— 这台机器跑不到这个速率."
      echo
      echo "  不限速实测 ${cap_gp} Mbps, 而扫描已经走到 ${SLOW_AT} Mbit."
      echo "  可能的原因："
      echo "    1) 这条线没有限速器, 到 ${cap_gp} 就到顶了 —— 那本来就不需要整形"
      echo "    2) CPU 推不动这个速率的 HTB（核少的机器 3Gbit 以上会明显掉速）"
      echo "    3) 对端不够快, 换公共节点（选对端时回车）再试一次"
    else
      warn "对端速率不够, 无法测出本机限速器 —— 已暂停调优."
      echo
      echo "  怎么办："
      echo "    1) 换一个更快的对端. 对端带宽必须明显高于本机（${nominal}Mbps）"
      echo "    2) 直接用公共节点（选对端时回车）, Leaseweb 机房带宽足够"
      echo "    3) 如果确定本机带宽没那么高, 重跑时把带宽填成实际值"
    fi
    echo
    info "基础调优（拥塞控制 / 缓冲区）已生效."
    traffic_report
    return 2
  fi

  # 粗扫只能定位到「拐点在 LAST_OK 与 BROKE_AT 之间」, 区间宽度就是步长.
  # 在这个区间用 1/4 步长再扫一遍, 把真实上限找准 —— 步长 20 时能多挖回十几 Mbps.
  # 只要粗扫区间里还存在未测过的整数档位就细扫. 小带宽机器常见
  # 26M 干净、28M 丢包；旧的 >5 条件会漏掉 27M, 白白多退 1M.
  if [ "$refine" = 1 ] && [ -n "$LAST_OK" ] && [ -n "$BROKE_AT" ] && [ $(( BROKE_AT - LAST_OK )) -gt 1 ]; then
    local fine coarse_broke
    coarse_broke=$BROKE_AT                       # 先存下粗扫的上界, 下面会被 scan_range 重置
    # 下限 1 而不是 5 —— 步长本身现在按区间宽度推, 小机器上可能只有 1-2,
    # 硬性抬到 5 会让细扫比粗扫还粗.
    # 精修步长默认取粗扫步长的 1/4 —— 前提是拐点区间只有一个步长宽(相邻两档).
    # 「首档丢包 -> 向下退 25% 找对照」会把区间撑宽: 10G 机器上退 25% 就是
    # 2000 Mbit, 区间变成 10 个步长, 按 1/4 步长走就是 43 档, 每档 12 秒 ×
    # 6-7 Gbps ≈ 10 GB. 客户实报过一次跑掉 400 GB.
    # 这里按区间实际宽度把精修压到 8 档以内. 正常情况区间 <= 一个步长,
    # 宽/8 必然小于 步长/4, 取大值后结果不变（有回归用例钉住档位序列）.
    local _w=$(( coarse_broke - LAST_OK ))
    fine=$(( step / 4 ))
    [ $(( (_w + 7) / 8 )) -gt "$fine" ] && fine=$(( (_w + 7) / 8 ))
    [ "$fine" -lt 1 ] && fine=1
    echo
    info "Knee between ${LAST_OK} and ${coarse_broke}, refining with step ${fine}"
    printf '  %-10s %12s %9s %8s  %s\n' "Rate/Mbit" "Goodput/Mbps" "Retrans" "Loss%" "Verdict"
    BROKE_AT=""
    scan_range $(( LAST_OK + fine )) $(( coarse_broke - fine )) "$fine"
    # 细扫在更细的档位上可能都不触发阈值(拐点就在 coarse_broke 那一档).
    # 不恢复的话 BROKE_AT 是空的, 后面会误判成"未检测到限速器"而不整形 ——
    # 群里实测碰到: 粗扫 536 已经 0.47%-0.63% 丢包, 细扫 521/526/531 都干净,
    # 结果报"未检测到限速器". 粗扫的结论必须保留.
    [ -n "$BROKE_AT" ] || BROKE_AT="$coarse_broke"
  fi

  echo
  trap - INT TERM HUP
  restore_qdisc
  echo
  local knee="$LAST_OK"
  [ -n "$knee" ] || { warn "no usable rate measured, check that the peer is reachable"; return 2; }

  # 扫到区间上界都没有丢包跳变 —— 说明扫描范围内不存在限速器.
  # 早期版本把区间上界当成拐点, 于是给一台根本没有 policer 的机器套了个上限
  # (用户一台 500M 标称的机器被设成 585, 而它实际能跑 9.3 Gbps).
  if [ -z "$BROKE_AT" ]; then
    echo
    # 【覆盖检查必须放在最前面】. 有档位下发失败或被跳过时, "扫到上界" 这个
    # 前提就不成立 —— 既不能说"没有限速器"(NO_KNEE), 也不能说"拐点不在这个
    # 范围内"(OUT_OF_RANGE): 中断证明不了后者. 早期版本把 OUT_OF_RANGE 排在
    # 前面并直接 return, 于是自动扫描里中间档 tc 失败仍输出"扫到上界 48".
    # SCANNED_TO 不在这里用 —— 那个名字会被读成"已经扫到这里", 换成 REQUESTED_TO.
    if [ "$SCAN_INCOMPLETE" = 1 ]; then
      warn "扫描没跑完: 有档位下发失败或对端无结果, 实际只验证到 ${SCAN_MAX_OK:-无} Mbit."
      echo "  这个结果判不出有没有限速器（请求的上界 ${hi} Mbit 并未验证）."
      echo "  换个更空闲的对端重测, 或用 --from/--to 缩小范围."
      mkdir -p "$STATE_DIR"
      printf 'INCONCLUSIVE=1\nSCAN_VERIFIED=%s\nVERIFIED_TO=%s\nREQUESTED_TO=%s\n' \
        "$SCAN_VERIFIED" "${SCAN_MAX_OK:-0}" "$hi" > "$STATE_DIR/sweep.result"
      traffic_report
      return 3
    fi
    if [ -n "$ug" ] && awk -v l="${ulp:-0}" -v t="$thresh" 'BEGIN{exit !(l > t)}'; then
      # 不限速时明明高丢包, 说明限速器确实存在, 只是不在扫描范围内 ——
      # 这跟"没有限速器"是两回事, 不能混为一谈.
      warn "不限速时丢包 ${ulp}%, 但扫到上界 ${hi} Mbit 仍未定位到拐点."
      echo "  限速器应该存在, 只是不在本次扫描范围内. 可以扩大范围重扫:"
      echo "    $(disp) sweep --peer <对端> --from ${hi} --to $(( hi * 2 ))"
      mkdir -p "$STATE_DIR"
      printf 'OUT_OF_RANGE=1\nSCANNED_TO=%s\nVERIFIED_TO=%s\n' \
        "$hi" "${SCAN_MAX_OK:-0}" > "$STATE_DIR/sweep.result"
      traffic_report
      return 3
    fi
    warn "扫到 ${hi} Mbit 仍未出现丢包跳变, 未检测到限速器."
    mkdir -p "$STATE_DIR"
    printf 'NO_KNEE=1\nSCANNED_TO=%s\nVERIFIED_TO=%s\n' \
      "$hi" "${SCAN_MAX_OK:-0}" > "$STATE_DIR/sweep.result"
    traffic_report
    return 3
  fi
  # 安全余量按标称带宽分档. 早期用固定 20Mbit, 在 300M 机器上白丢 19Mbps
  # （实测 300 档重传比 280 档还少）, 说明一个数字套所有带宽不合理.
  [ -n "$margin" ] || margin=$(calc_margin "$nominal")
  local final=$(( knee - margin )); [ "$final" -lt 1 ] && final=$knee
  mkdir -p "$STATE_DIR"; echo "KNEE=$knee"$'\n'"RECOMMEND=$final" > "$STATE_DIR/sweep.result"
  # 一键流程里这些数字由 wizard 在「结果」里统一呈现, 这里只出执行日志
  if [ "$WIZARD" = 1 ]; then
    ok "Knee ${knee} Mbit, margin ${margin} Mbit -> shape at ${final} Mbit"
    return 0
  fi
  ok "实测上限 ${knee} Mbit, 按 ${nominal}M 档位退 ${margin} 余量 → 建议整形值 ${final} Mbit"
  echo
  echo "  应用: $(disp) shape --rate $final"
  echo "  (扫描本身不会改变整形配置, 上面这条才会)"
  traffic_report
}

# ── 验证与状态 ──────────────────────────────────────────────────────────────
cmd_status(){
  local iface; iface=$(detect_iface)
  echo "── Current configuration ──"
  kv "Kernel"      "$(uname -r)"
  kv "Congestion"  "$(sysctl -n net.ipv4.tcp_congestion_control 2>/dev/null)"
  kv "Default qdisc" "$(sysctl -n net.core.default_qdisc 2>/dev/null)"
  kv "Active qdisc" "$(tc qdisc show dev "$iface" 2>/dev/null | head -1 | awk '{print $2}')"
  kv "Egress shaper" "$(first_or "$(r=$(tc_rate_mbit "$(tc class show dev "$iface" 2>/dev/null)") && echo "${r} Mbit")" none)"
  kv "tcp_rmem"    "$(sysctl -n net.ipv4.tcp_rmem 2>/dev/null | tr '\t' ' ')"
  kv "tcp_wmem"    "$(sysctl -n net.ipv4.tcp_wmem 2>/dev/null | tr '\t' ' ')"
  kv "tcp_mem"     "$(sysctl -n net.ipv4.tcp_mem 2>/dev/null | awk '{printf "%.0fM/%.0fM/%.0fM", $1*4/1024,$2*4/1024,$3*4/1024}')"
  kv "Backlog"     "$(sysctl -n net.core.netdev_max_backlog 2>/dev/null)"
  kv "initcwnd"    "$(ip route show default | grep -oE 'initcwnd [0-9]+' || echo '默认(10)')"
  kv "tcpfit conf" "$([ -f "$SYSCTL_FILE" ] && echo applied || echo absent)"
  kv "Shaper svc"  "$(systemctl is-enabled tcpfit-qdisc.service 2>/dev/null || echo 未安装)"
  kv "Snapshot"    "$([ -f "$SNAPSHOT" ] && echo "$SNAPSHOT" || echo 无)"
  echo
  echo "── Health ──"
  local out rt
  out=$(awk '/^Tcp: [0-9]/{print $12, $13}' /proc/net/snmp)
  rt=$(echo "$out" | awk '{if($1>0) printf "%.3f%%", $2*100/$1; else print "n/a"}')
  kv "Retrans (boot)" "$rt  (cumulative since boot; use Verify for current)"
  kv "qdisc drops" "$(first_or "$(grep -oP 'dropped \K[0-9]+' <<<"$(tc -s class show dev "$iface" 2>/dev/null)")" n/a)"
  kv "Memory"      "$(free -m | awk '/Mem:/{print "已用 "$3"MB / 可用 "$7"MB / 共 "$2"MB"}')"
  kv "Swap"        "$(free -m | awk '/Swap:/{if($2==0) print "none (recommended on low-memory hosts)"; else print $3"/"$2" MB"}')"
  # grep -c 无匹配时输出 0 但退出码 1, 不能用 || 兜底, 否则会打印两个 0
  kv "OOM (1h)"    "$(journalctl --since '-1 hour' 2>/dev/null | grep -c 'oom-kill') in last hour"
}

# 验证「本机端口能力」. 刻意用近端对端 —— 测的是服务器出口能发多快、
# 整形有没有生效, 不是到国内的速度（那取决于线路质量, 见 cmd_cntest）.
# 实测 + 判定拆开：一键流程要把执行日志（英文）和结论（中文）分在两段里打印.
VS1=""; VG1=""; VR1=""; VS4=""; VG4=""; VR4=""; VDUR=10
verify_measure(){
  local peer="$1" res
  VS1=""; VG1=""; VR1=""; VS4=""; VG4=""; VR4=""
  res=$(run_iperf "$peer" "$VDUR" 1); [ -n "$res" ] && {
    VS1=$(echo "$res"|awk '{print $1}'); VR1=$(echo "$res"|awk '{print $2}')
    VG1=$(echo "$res"|awk '{print $3}'); [ -n "$VG1" ] || VG1="$VS1"
  }
  sleep 3
  res=$(run_iperf "$peer" "$VDUR" 4); [ -n "$res" ] && {
    VS4=$(echo "$res"|awk '{print $1}'); VR4=$(echo "$res"|awk '{print $2}')
    VG4=$(echo "$res"|awk '{print $3}'); [ -n "$VG4" ] || VG4="$VS4"
  }
}

# 打印验证结果表 + 结论. $1 = 当前整形值(Mbit, 可空)
#
# 判定一律用丢包率, 不用绝对重传次数 —— 同样 14574 次, 300M 机上是 5.6% 的灾难,
# 9G 机上只有 0.19% 属正常. sweep 早就改成丢包率了, verify 这里当时漏了同步,
# 结果给一台 5Gbps 的机器报"重传偏高, 整形值可能设高了", 而那台根本没装整形.
#
#   < 0.05%    干净
#   0.05-0.5%  略高, 通常不影响
#   0.5-1%     偏高, 值得查
#   > 1%       很糟, 多半撞了限速器或链路有问题
# 参照: 实测干净的机器在 0.0013%-0.0017%, 真撞限速器是 1.35%-6.50%.
verify_verdict(){
  local target="${1:-}" lp1="" lp4=""
  [ -n "$VS1" ] && [ -n "$VR1" ] && lp1=$(loss_pct "$VR1" "$VS1" "$VDUR")
  [ -n "$VS4" ] && [ -n "$VR4" ] && lp4=$(loss_pct "$VR4" "$VS4" "$VDUR")
  echo "  验证"
  printf '      %s %s %s %s\n' "$(_pad "" 14)" "$(_rpad "吞吐 Mbps" 12)" "$(_rpad "重传" 9)" "$(_rpad "丢包率" 10)"
  printf '      %s %s %s %s\n' "$(_pad "单流" 14)"     "$(_rpad "${VG1:-测试失败}" 12)" "$(_rpad "${VR1:--}" 9)" "$(_rpad "${lp1:+${lp1}%}" 10)"
  printf '      %s %s %s %s\n' "$(_pad "4 流并发" 14)" "$(_rpad "${VG4:-测试失败}" 12)" "$(_rpad "${VR4:--}" 9)" "$(_rpad "${lp4:+${lp4}%}" 10)"
  echo
  # 吞吐和整形值比, 给结论而不是丢一堆数字
  if [ -n "$VG4" ] && [ -n "$target" ] && [ "$target" -gt 0 ] 2>/dev/null; then
    local pct; pct=$(awk -v a="$VG4" -v b="$target" 'BEGIN{printf "%.0f", a*100/b}')
    if   [ "$pct" -ge 90 ] 2>/dev/null; then ok "达到整形值的 ${pct}%, 端口能力正常"
    elif [ "$pct" -ge 75 ] 2>/dev/null; then info "达到整形值的 ${pct}%, 偏低但可接受（对端可能被其他人占用）"
    else warn "只达到整形值的 ${pct}%, 建议换个对端重测"; fi
  fi
  [ -n "$lp4" ] || return 0
  # 没有整形时不能说"整形值设高了" —— 用户会被指去调一个根本不存在的东西
  local advice
  if [ -n "$target" ] && [ "$target" -gt 0 ] 2>/dev/null; then
    advice="整形值可能设高了, 可以重跑菜单 3 重新找拐点"
  else
    advice="这台没有应用整形. 高丢包来自链路本身或未被识别的限速器, 可以跑菜单 3 试着扫一次拐点"
  fi
  if   awk -v l="$lp4" 'BEGIN{exit !(l < 0.05)}'; then ok   "丢包 ${lp4}%, 链路干净"
  elif awk -v l="$lp4" 'BEGIN{exit !(l < 0.5)}';  then ok   "丢包 ${lp4}%, 略高, 通常不影响"
  elif awk -v l="$lp4" 'BEGIN{exit !(l < 1)}';    then warn "丢包 ${lp4}%, 偏高 —— ${advice}"
  else                                                 warn "丢包 ${lp4}%, 很糟 —— ${advice}"; fi
}

cmd_verify(){
  local peer="" peer_name="" peer_rtt=""
  while [ $# -gt 0 ]; do
    case "$1" in --peer) peer="$2"; shift 2 ;; --name) peer_name="$2"; shift 2 ;; *) shift ;; esac
  done
  local iface shaper; iface=$(detect_iface)
  shaper=$(tc_rate_mbit "$(tc class show dev "$iface" 2>/dev/null)")

  echo
  printf '  %s本机端口能力验证%s\n' "$bold" "$plain"
  rule
  echo "  测的是：服务器出口能发多快、整形有没有生效"
  echo "  不测：到国内的速度（那取决于线路质量, 跟服务器配置无关）"
  echo

  if [ -z "$peer" ]; then
    warn "没有可用对端, 只显示配置"
    cmd_status; return 0
  fi
  peer_rtt=$(ping $IP_FAMILY -c 2 -q -W 2 "$peer" 2>/dev/null | awk -F'/' '/rtt|round-trip/{printf "%.0f", $5}')
  printf "  对端    %s   RTT %sms   端口 %s\n" "$peer" "${peer_rtt:-?}" "$PEER_PORT"
  printf "  整形    %s\n" "$(first_or "${shaper:+${shaper} Mbit}" 未设置)"
  echo
  command -v iperf3 >/dev/null || { warn "无 iperf3, 跳过实测"; return 0; }

  verify_measure "$peer"
  verify_verdict "$shaper"
  rule
  # 退出码要反映实测结果. 文件开头的约定是 2 = 实测失败, 而早期版本
  # 末尾的 rule 总是成功, 于是两项测速全失败也返回 0 ——
  # 上层脚本/监控拿退出码判断时会把彻底失败当成通过.
  if [ -z "$VG1" ] && [ -z "$VG4" ]; then
    warn "两项实测都没拿到结果, 无法验证（对端不可达/占线?）"
    return 2
  fi
  return 0
}

# ── 检查更新 ────────────────────────────────────────────────────────────────
cmd_update(){
  need_root
  command -v curl >/dev/null || die "需要 curl"
  # 菜单调进来时带 --from-menu: 更新完要用新版本 exec 掉自己, 否则用户在同一个
  # 菜单里接着操作, 跑的仍是内存里的旧代码.
  local from_menu=0
  [ "${1:-}" = "--from-menu" ] && { from_menu=1; shift; }
  info "检查更新…"
  local latest
  # 只看 release, 不看 main —— main 可能领先于任何已发布版本
  latest=$(curl -fsSL --max-time 10 "https://api.github.com/repos/Kylin010/tcpfit/releases/latest" 2>/dev/null \
           | grep -m1 '"tag_name"' | sed 's/.*"tag_name"[[:space:]]*:[[:space:]]*"v\{0,1\}\([^"]*\)".*/\1/')
  [ -n "$latest" ] || die "查不到最新版本, 检查网络或稍后再试" 2

  if [ "$latest" = "$VERSION" ]; then ok "已是最新版本 v$VERSION"; return 0; fi
  # 用 sort -V 比版本号, 字符串比较会把 0.3.10 判成小于 0.3.9
  if [ "$(printf '%s\n%s\n' "$VERSION" "$latest" | sort -V | tail -1)" = "$VERSION" ]; then
    ok "当前 v$VERSION 比已发布的 v$latest 还新（开发版）"; return 0
  fi

  echo
  _conf "当前版本" "v$VERSION"
  _conf "最新版本" "v$latest"
  _conf "更新说明" "https://github.com/Kylin010/tcpfit/releases/tag/v$latest"
  echo
  confirm "  现在更新？" y || { info "已取消"; return 0; }

  # 从 release 下, 用发布的 SHA256SUMS 校验. 只对比 tcpfit.sh 那一行 ——
  # SHA256SUMS 里还有 install.sh, 直接 sha256sum -c 会因为文件不在而失败.
  local dl; dl=$(mktemp -d)
  local base="https://github.com/Kylin010/tcpfit/releases/download/v$latest"
  if ! curl -fsSL --max-time 60 "$base/tcpfit.sh" -o "$dl/tcpfit.sh"; then
    rm -rf "$dl"; die "下载失败" 2
  fi
  if command -v sha256sum >/dev/null && curl -fsSL --max-time 20 "$base/SHA256SUMS" -o "$dl/SHA256SUMS"; then
    if ! ( cd "$dl" && grep ' tcpfit\.sh$' SHA256SUMS | sha256sum -c - >/dev/null 2>&1 ); then
      rm -rf "$dl"; die "SHA256 校验不通过, 未更新" 2
    fi
    info "SHA256 校验通过"
  else
    warn "取不到 SHA256SUMS 或没有 sha256sum, 退回版本号校验"
  fi
  if ! { starts_with "$(head -1 "$dl/tcpfit.sh" 2>/dev/null)" '#!' && grep -q "^VERSION=\"$latest\"" "$dl/tcpfit.sh"; }; then
    rm -rf "$dl"; die "下载的文件校验不通过, 未更新" 2
  fi
  # 不能原地覆盖 —— 正在执行的就是 $SELF_PATH, 而 bash 是按文件偏移增量读脚本的,
  # 原地改写有可能让它读到新文件的错位内容（两个版本长度还不一样）.
  # 先写同目录的 .new 再 mv: rename 是原子的, 换新 inode, 旧 inode 对当前进程保持有效.
  if ! install -m 755 "$dl/tcpfit.sh" "${SELF_PATH}.new" || ! mv -f "${SELF_PATH}.new" "$SELF_PATH"; then
    rm -f "${SELF_PATH}.new"; rm -rf "$dl"; die "写入 $SELF_PATH 失败" 2
  fi
  rm -rf "$dl"
  ok "已更新到 v$latest"
  info "配置和快照不受影响."

  # 关键: 磁盘上换了, 但当前进程内存里跑的还是旧代码.
  # 早期版本这里只打一句"重跑一次调优", 用户就在同一个菜单里按 1 —— 跑的仍是旧版本,
  # 于是"更新了但 bug 还在". 有客户真踩过, 排查了很久才定位到是这里.
  if [ "$from_menu" = 1 ]; then
    info "以新版本重启…"
    echo
    exec "$SELF_PATH" menu
  fi
  warn "当前进程跑的仍是 v${VERSION} 的代码, 重新运行 tcpfit 才会用上新版本."
}

# ── 交互式菜单 ──────────────────────────────────────────────────────────────
#
# 设计原则：用户只需要回答"这机器干什么用的", 其余全部自动.
# 尤其是 iperf3 对端 —— 让用户自己挑服务器是最大的使用门槛, 这里自动 ping 一圈选最近的.

# 公共 iperf3 服务器池. 挑选标准：长期在线、允许匿名测试、地理分布覆盖主要机房区域.
# 公共 iperf3 测速节点池. 格式: 主机|地区|提供商
#
# 这些是第三方免费提供的公共测试服务器, sweep 会向它们发送测试流量.
# 节点来源与实测稳定性（2026-08 在欧洲机器上各测 3 次握手）：
#   Leaseweb   全球机房, 18 节点中 15 个 3/3 —— 最稳, 优先用
#   Clouvider  5 节点中仅 2 个 3/3 —— 时好时坏, 作备选
#   OVH        新加坡节点 3/3
# 注: 早期用 timeout 15 测稳定性, 对 280ms+ 的远节点连握手都不够, 误判成不可用.
# 判定节点好坏不能用固定超时 —— 和 RTT 一刀切是同一类错误.
# 完整公共列表见 https://iperf3serverlist.net
PEER_POOL="
speedtest.hkg12.hk.leaseweb.net|香港|Leaseweb
speedtest.sin1.sg.leaseweb.net|新加坡|Leaseweb
sgp.proof.ovh.net|新加坡|OVH
speedtest.syd12.au.leaseweb.net|悉尼|Leaseweb
speedtest.tyo11.jp.leaseweb.net|东京|Leaseweb
speedtest.fra1.de.leaseweb.net|法兰克福|Leaseweb
speedtest.ams2.nl.leaseweb.net|阿姆斯特丹|Leaseweb
ams.speedtest.clouvider.net|阿姆斯特丹|Clouvider
speedtest.lon12.uk.leaseweb.net|伦敦|Leaseweb
lon.speedtest.clouvider.net|伦敦|Clouvider
speedtest.lax12.us.leaseweb.net|洛杉矶|Leaseweb
speedtest.sfo12.us.leaseweb.net|旧金山|Leaseweb
speedtest.sea11.us.leaseweb.net|西雅图|Leaseweb
speedtest.dal13.us.leaseweb.net|达拉斯|Leaseweb
speedtest.chi11.us.leaseweb.net|芝加哥|Leaseweb
speedtest.nyc1.us.leaseweb.net|纽约|Leaseweb
speedtest.mia11.us.leaseweb.net|迈阿密|Leaseweb
speedtest.mtl2.ca.leaseweb.net|蒙特利尔|Leaseweb
"

# 自动挑选对端：先按 RTT 排序, 再逐个验证 iperf3 真的能用（公共服务器常年占线）
auto_pick_peer(){
  local best="" cand rtt name line
  # 兜底: 命令行直接跑 sweep/verify 的人不走向导, 拿不到那边的安装提示.
  # 没有 ping 时下面每个节点都取不到 RTT, sorted 为空 → 静默 return 1,
  # 调用方报 "公共测速服务器暂时都不可用" —— 服务器是无辜的, 得说真话.
  if command -v ping >/dev/null 2>&1 && ! ping_supports_4; then
    warn "本机的 ping 不认 -4（多半是 GNU inetutils 版）, 无法自动选择对端." >&2
    warn "  换成标准版:  apt install -y iputils-ping  /  dnf install -y iputils" >&2
    warn "  或指定对端:  --peer <iperf3服务器>" >&2
    echo ""; return 1
  fi
  if ! command -v ping >/dev/null 2>&1; then
    warn "本机缺少 ping, 无法自动选择对端." >&2
    warn "  安装:  apt install -y iputils-ping   /   dnf install -y iputils" >&2
    warn "  或指定对端:  --peer <iperf3服务器>" >&2
    echo ""; return 1
  fi
  info "自动选择测速对端（测的是本机端口上限, 越近越准）…" >&2
  # 并行 ping 全部节点. 串行时 17 个节点 × 最长 4 秒 = 最坏 68 秒, 用户干等.
  local sorted="" prov tmpd
  tmpd=$(mktemp -d)
  while IFS='|' read -r cand name prov; do
    [ -z "$cand" ] && continue
    ( r=$(ping $IP_FAMILY -c 2 -q -W 2 "$cand" 2>/dev/null | awk -F'/' '/rtt|round-trip/{printf "%.0f", $5}')
      [ -n "$r" ] && echo "$r $cand $name $prov" > "$tmpd/$cand" ) &
  done <<< "$PEER_POOL"
  wait
  sorted=$(cat "$tmpd"/* 2>/dev/null); rm -rf "$tmpd"
  [ -n "$sorted" ] || { echo ""; return 1; }
  # RTT 分级：sweep 测的是本机端口上的限速器, 对端越近越准.
  #   ≤ideal  最佳, 链路干扰可忽略
  #   ≤accept 可用, 但要提醒用户结果可能偏保守
  #   >accept 拒绝, 宁可失败也不给错误结论
  # 早期只有一个 60ms 硬阈值, 结果香港机器上新加坡 61ms 被卡掉、整个流程失败 —— 太死板.
  local ideal="${NETTUNE_PEER_IDEAL_RTT:-50}"
  local accept="${NETTUNE_PEER_MAX_RTT:-100}"
  local fallback="" fallback_rtt="" limit="$accept" FAR_SKIPPED=0
  while read -r rtt cand name prov; do
    [ -z "$cand" ] && continue
    if [ "$rtt" -gt "$limit" ] 2>/dev/null; then
      printf '  %-34s %-10s %-10s RTT %-6s %s\n' "$cand" "$name" "$prov" "${rtt}ms" "too far, skipped" >&2
      FAR_SKIPPED=1
      continue
    fi
    printf '  %-34s %-10s %-10s RTT %-6s ' "$cand" "$name" "$prov" "${rtt}ms" >&2
    # 先探端口, 把"根本不跑 iperf3/被墙"和"跑着但占线"分开 ——
    # 早期两者都报"占线", 用户完全看不出真实原因.
    # 必须轮换: 只探 5201 的话, 出站封了 5201 的机房上所有节点都会被误判(见 PROBE_PORTS).
    if ! probe_peer_port "$cand"; then
      echo "port closed (tried $PROBE_PORTS)" >&2; continue
    fi
    local pport="$PROBE_PORT_OK"
    [ "$pport" = 5201 ] || printf '%s ' "$(_c '0;33' "5201→$pport")" >&2
    # 没装 iperf3 时无法做占线探测（iperf3 要等确认之后才装）,
    # 降级成"端口通就算可用". 选错了也不致命 —— run_iperf 本身会换端口重试.
    if ! command -v iperf3 >/dev/null 2>&1; then
      printf '%s\n' "$(_c '0;32' "reachable (port $pport)")" >&2
      echo "$cand:$pport"; return 0
    fi
    # 这些公共节点都开十个 iperf3 实例（公共列表里标的就是端口范围）.
    # 早期只试 5201, 等于放着 9 个空闲实例不用去跟全世界抢一个, 动不动就"占线".
    # 从预检探通的那个端口起试 —— 5201 被封时能省掉一次 25 秒的超时等待.
    local gp="" try
    for try in $(port_order "$pport"); do
      if timeout $TIMEOUT_FG 25 iperf3 $IP_FAMILY -c "$cand" -p "$try" -t 3 -P 1 >/dev/null 2>&1; then gp="$try"; break; fi
    done
    if [ -n "$gp" ]; then
      if [ "$rtt" -le "$ideal" ] 2>/dev/null; then
        echo "${green}available${plain} (port $gp)" >&2; best="$cand:$gp"; break
      fi
      echo "available (port $gp, distant — held as fallback)" >&2
      [ -z "$fallback" ] && { fallback="$cand:$gp"; fallback_rtt="$rtt"; }
    else
      echo "all $(echo $PORT_POOL | wc -w) ports busy" >&2
    fi
    sleep 2
  done <<< "$(echo "$sorted" | sort -n)"

  if [ -z "$best" ] && [ -n "$fallback" ]; then
    best="$fallback"
    echo >&2
    warn "最近的可用对端是 ${fallback_rtt}ms（理想是 ${ideal}ms 以内）." >&2
    warn "距离越远, 链路本身的丢包抖动越会混进测量, 拐点可能偏保守." >&2
    warn "结果仍然可用, 只是可能没榨到极限." >&2
  fi

  # 全部节点都超过 accept 时, 早期版本直接失败 —— 国内机器上 18 个节点无一幸免,
  # 整个流程走不下去（issue #4）. 但"远"只是让拐点偏保守, 不是测不了.
  # 所以改成: 说清楚代价, 然后放开距离限制再扫一遍.
  if [ -z "$best" ] && [ -z "$fallback" ] && [ "$FAR_SKIPPED" = 1 ]; then
    echo >&2
    warn "所有公共节点都超过 ${accept}ms —— 本机多半在国内, 或线路绕远." >&2
    warn "距离远会让链路本身的丢包混进测量, 扫出的拐点偏保守（宁可低不冒高）." >&2
    warn "结果仍然可用. 想更准就自己在近处开一台 iperf3 -s, 用 --peer 指定." >&2
    echo >&2
    info "放开距离限制, 用最近的节点重新试…" >&2
    limit=100000
    while read -r rtt cand name prov; do
      [ -z "$cand" ] && continue
      printf '  %-34s %-10s %-10s RTT %-6s ' "$cand" "$name" "$prov" "${rtt}ms" >&2
      if ! probe_peer_port "$cand"; then
        echo "port closed (tried $PROBE_PORTS)" >&2; continue
      fi
      local pport2="$PROBE_PORT_OK"
      if ! command -v iperf3 >/dev/null 2>&1; then
        printf '%s\n' "$(_c '0;32' "reachable (port $pport2)")" >&2
        echo "$cand:$pport2"; return 0
      fi
      local gp2="" try2
      for try2 in $(port_order "$pport2"); do
        if timeout $TIMEOUT_FG 25 iperf3 $IP_FAMILY -c "$cand" -p "$try2" -t 3 -P 1 >/dev/null 2>&1; then gp2="$try2"; break; fi
      done
      if [ -n "$gp2" ]; then
        echo "$(_c '0;33' "available (port $gp2, 距离超标但可用)")" >&2
        best="$cand:$gp2"; break
      fi
      echo "all $(echo $PORT_POOL | wc -w) ports busy" >&2
      sleep 2
    done <<< "$(echo "$sorted" | sort -n | head -5)"
  fi

  if [ -z "$best" ]; then
    warn "公共测速服务器都不可用（要么端口不通, 要么全部占线）." >&2
    warn "公共服务器一次只接一个测试, 等几分钟再试通常就有了." >&2
    warn "或者自己开一台近处的机器跑 iperf3 -s, 然后用 --peer 指定." >&2
    return 1
  fi
  echo "$best"
}

# 验证对端路径是否干净. RTT 只是代理指标 —— 真正要的是路径没有丢包干扰测量.
# 用标称带宽的 40% 跑一次：这个速率远低于任何限速器, 此时还有明显重传,
# 就说明是链路本身在丢包, 拿它测拐点必然测偏.
validate_peer(){
  local peer="$1" nominal="$2" iface="$3"
  # 低带宽线路不能硬抬到 20M: 15M 线路会被验证流量自己打穿.
  local rate=$(( nominal * 40 / 100 )); [ "$rate" -lt 1 ] && rate=1
  qdisc_save "$iface" || { echo "skip:guard"; return 1; }
  # 早期版本这里没有任何 trap: 中断就把机器留在标称 40% 的限速上, 直到重启
  trap 'qdisc_restore; exit 130' INT TERM HUP
  apply_test_shaper "$iface" "$rate" || { qdisc_restore; echo "unreachable"; return 1; }
  local res rt
  for _ in 1 2; do res=$(run_iperf "$peer" 8 2); [ -n "$res" ] && break; sleep 5; done
  trap - INT TERM HUP
  qdisc_restore
  [ -n "$res" ] || { echo "unreachable"; return 1; }
  local sg gp lp
  sg=$(echo "$res" | awk '{print $1}'); rt=$(echo "$res" | awk '{print $2}')
  gp=$(echo "$res" | awk '{print $3}'); [ -n "$gp" ] || gp="$sg"
  lp=$(loss_pct "$rt" "$sg" 8)
  # 对端连 40% 速率都跑不到, 说明它本身就比本机慢, 拿它测限速器毫无意义.
  # 只有低重传时才能这样判断；高重传造成的低吞吐属于脏路径, 不是慢对端.
  if awk -v g="$gp" -v r="$rate" -v l="$lp" \
     'BEGIN{exit !(g < r*0.7 && l <= 0.05)}' 2>/dev/null; then
    echo "slow:$gp/$rate"; return 1
  fi
  # 低速率下丢包率应该接近 0. 比 sweep 更严(0.05% vs 0.1%), 因为跑的是 40% 速率.
  if awk -v l="$lp" 'BEGIN{exit !(l > 0.05)}' 2>/dev/null; then echo "dirty:${rt}(${lp}%)"; return 1; fi
  echo "clean:$rt"
}

# 曾用它清"超前输入"防止杂散回车误答, 但它会把管道/脚本喂进来的合法输入
# 一起吃掉（实测卡在带宽提示不动）, 手速快的用户也会中招.
# 主操作默认值改成 y 之后, 杂散回车本身已无害, 所以不再调用.
flush_input(){ :; }

# 只给"按任意键返回"用. 调优一跑十几分钟, 其间用户随手按的键留在 tty 缓冲里,
# 提示符一出来就被瞬间吃掉 -> 结果页面没看见就回了菜单.
# 故意不用在 ask/confirm 上, 原因见上面 flush_input 的注释.
# 坑: read -t 0 只判断"有没有数据", 不消费数据, 用它是死循环; 超时必须非零.
# 坑2: 重定向从左往右生效, </dev/tty 要写在 2>/dev/null 后面, 否则无 tty 时报错漏出来.
drain_tty(){ while read -rsn1 -t 0.05 2>/dev/null </dev/tty; do :; done; return 0; }

# 能不能打开控制终端. 重定向顺序同下面 ask 的注释: 2>/dev/null 必须在前.
have_tty(){ : 2>/dev/null </dev/tty; }

ask(){  # ask "问题" "默认值"  -> 回显用户输入或默认值
  local q="$1" d="${2:-}" a
  if [ -n "$d" ]; then printf '%s [%s]: ' "$q" "$d" >&2; else printf '%s: ' "$q" >&2; fi
  # 2>/dev/null 必须写在 </dev/tty 前面（重定向从左往右生效）——
  # 否则无 tty 时（管道调用、CI、ssh host tcpfit < /dev/null）
  # 每问一次就往屏幕漏一行 "/dev/tty: No such device or address".
  read -r a 2>/dev/null </dev/tty || a=""
  echo "${a:-$d}"
}

# confirm "问题" [默认]  -> 0=是 1=否. 默认 y 时空回车即同意.
# 主操作（如"开始调优？"）必须默认 y —— 用户就是为这个来的,
# 一个杂散回车不该让整个流程静默取消.
confirm(){
  local d="${2:-n}" a p
  [ "$d" = y ] && p="(Y/n)" || p="(y/N)"
  a=$(ask "$1 $p" "$d")
  [[ "$a" =~ ^[Yy] ]]
}

# 框宽固定 48 列. 每行按显示宽度补齐后再包边框 ——
# 手写空格对不齐, 因为 CJK 占 2 列而框线字符占 1 列.
BOX_W=56
_row(){ # _row "<内容>" [颜色代码]
  local txt="$1" col="${2:-}" pad
  pad=$(( BOX_W - $(_dispw "$txt") ))
  [ "$pad" -lt 0 ] && pad=0
  if [ -n "$col" ]; then printf '│\033[%sm%s\033[0m%*s│\n' "$col" "$txt" "$pad" ""
  else printf '│%s%*s│\n' "$txt" "$pad" ""; fi
}
_sep(){ printf '│'; printf '─%.0s' $(seq $BOX_W); printf '│\n'; }
_top(){ printf '╔'; printf '─%.0s' $(seq $BOX_W); printf '╗\n'; }
_bot(){ printf '╚'; printf '─%.0s' $(seq $BOX_W); printf '╝\n'; }

# 菜单条目：中文名、英文名、耗时三列各自按显示宽度补齐.
# 手写空格必然错位 —— 中文占 2 列,"~10 min" 这种右列一长就把右边框顶出去.
_item(){ # _item <编号> <中文> <英文> [耗时]
  _row "$(printf '  %s. %s %s %s ' "$1" "$(_pad "$2" 10)" "$(_pad "$3" 30)" "$(_rpad "${4:-}" 8)")"
}

banner(){
  local iface cc shaper ram cores tuned
  iface=$(detect_iface)
  cc=$(sysctl -n net.ipv4.tcp_congestion_control 2>/dev/null)
  shaper=$(r=$(tc_rate_mbit "$(tc class show dev "$iface" 2>/dev/null)") && echo "${r}Mbit")
  ram=$(detect_ram_mb); cores=$(detect_cores)
  [ -f "$SYSCTL_FILE" ] && tuned="Tuned" || tuned="Stock"
  clear 2>/dev/null || true
  echo
  _top
  _row "$(printf '  tcpfit - VPS TCP Optimization%s ' "$(_rpad "v$VERSION" 23)")" '0;32'
  _row "  本脚本由 kylin010 编写和维护"
  _row "  github.com/Kylin010/tcpfit"
  _row "  VPS 补货频道  t.me/vpskuaibu"
  _row "  VPS 测评数据  spacevps.cc"
  _sep
  _row "  0. Exit"
  _item 1 "一键调优" "Auto-tune (recommended)"   "~10 min"
  _item 2 "基础调优" "Base tuning only"          "~1 min"
  _item 3 "拐点测试" "Policer sweep"             "~8 min"
  _item 4 "加 swap"  "Add swap (low-memory box)"
  _sep
  _item 5 "查看状态" "Status"
  _item 6 "端口验证" "Verify port capability"    "~1 min"
  _item 7 "回滚改动" "Rollback all changes"
  _item 8 "检查更新" "Check for updates"
  _item 9 "调优存档" "Tuning archives"
  _row "  u. 卸载 tcpfit / Uninstall"
  _bot
  printf "  %-9s %s core / %s MB / %s\n" "Machine" "$cores" "$ram" "$(uname -r)"
  printf "  %-9s cc=%s  shaper=%s  " "Network" "${cc:-?}" "${shaper:-none}"
  [ "$tuned" = Tuned ] && printf "${green}%s${plain}\n" "$tuned" || printf "${yellow}%s${plain}\n" "$tuned"
  local _stats; _stats=$(telemetry_line)
  [ -n "$_stats" ] && printf "  %-9s %s\n" "Runs" "$_stats"
}

# 一键全自动.
# 设计原则：所有要用户回答的东西集中在最前面（3 个问题）, 确认之后一路跑到底不再打断；
# 唯一的例外是流量: 实测出来的流量远超用户同意的数时, 停下来再问一次（默认否）.
# 执行阶段的日志用英文（都是参数名和数值, 中英混排反而看不清）, 结论用中文.
wizard(){
  local WIZARD=1 ARCH_INCLUDE_SWEEP=0 WIZARD_FAILED=0
  local ARCH_ROLE="" ARCH_BW="" ARCH_RTT="" ARCH_PEER=""
  local ram; ram=$(detect_ram_mb)
  echo
  echo "  ── 一键调优 ──"
  echo
  rule
  echo "  开始前的说明"
  echo
  echo "  改动前会把当前配置完整备份到"
  echo "      $(_c '1' "$SNAPSHOT")"
  echo "  包含拥塞控制、全部缓冲区参数、qdisc、路由等原始值."

  # 协议族: 默认 IPv4; 纯 v6 机器自动走 v6; 双栈才问.
  # 每个分支只说跟这台机器有关的话.
  # 早期版本在这里无条件打一句"测速默认走 IPv4. 检测不到 IPv4 会走 IPv6." ——
  # 本意是说明规则, 但纯 v4 机器(绝大多数)看到的就只有这一行, 而且没有任何一行
  # 确认"检测到了 v4". tcpfit 其他输出全是状态行, 用户就把"检测不到 IPv4"
  # 读成了对自己机器的判定, 以为脚本认错了. 有用户真这么报过.
  # 「没有 v4」不等于「有 v6」. 早期这里只判 v4, 判不出来就直接切 -6 ——
  # 于是 v4/v6 都检测不到的机器被切到一个根本不存在的协议族上, 之后每个节点
  # ping 都失败, 最后死在"公共测速服务器暂时都不可用". 客户实报过.
  if ! have_ipv4; then
    if have_ipv6; then
      IP_FAMILY="-6"
      ok "本机没有 IPv4, 测速走 IPv6"
    else
      # 两边都判不出来时保持默认的 -4: 绝大多数机器是纯 v4, 检测失误的可能
      # 远高于"真的两个都没有". 切到 v6 是必然失败, 留在 v4 至少还有机会.
      warn "检测不到 IPv4 也检测不到 IPv6 的默认路由, 仍按 IPv4 测速."
      warn "  若后面选不到对端, 用 --peer 手动指定."
    fi
  elif have_ipv6; then
    echo
    echo "  本机是 IPv4 + IPv6 双栈, 测速默认走 IPv4."
    echo
    local fam
    while true; do
      fam=$(ask "  用 v4 还是 v6？（回车 = v4）" "v4")
      case "$fam" in
        v4|V4|4) IP_FAMILY="-4"; break ;;
        v6|V6|6) IP_FAMILY="-6"; break ;;
        *) warn "  请输入 v4 或 v6" ;;
      esac
    done
    ok "测速走 IPv${IP_FAMILY#-}"
  fi

  # iperf3 单独放在最前面确认 —— 两个原因:
  #   1) 装包是会改系统的操作, 不该在用户点头之前做
  #   2) 选对端那一步要用 iperf3 做占线探测, 所以必须在三个问题之前就位
  local HAVE_IPERF3=1 QN=3
  if command -v iperf3 >/dev/null 2>&1; then
    echo "  iperf3 已经安装 $(iperf3 --version 2>/dev/null | awk 'NR==1{print $2}')"
  else
    echo "  确认带宽之前, tcpfit 需要安装 iperf3 才可以正常运行."
    echo
    if confirm "  安装？" y; then
      # 装之前先看锁, 被占的话直接说清楚, 别让用户干等一轮 apt 超时
      local _holder; _holder=$(apt_lock_holder)
      if [ -n "$_holder" ]; then
        set -- $_holder
        warn "包管理器正被 PID $1（$2, 已运行 $3）占用, 先不装了."
      else
        echo "    ────────────────────────────────────────"
        if   command -v apt-get >/dev/null; then apt-get update -qq && apt-get install -y iperf3
        elif command -v dnf     >/dev/null; then dnf install -y iperf3
        elif command -v yum     >/dev/null; then yum install -y epel-release; yum install -y iperf3
        # Alpine 的 busybox timeout/pkill 功能不全, 一并装 GNU 版
        elif command -v apk     >/dev/null; then apk add iperf3 coreutils procps
        else warn "认不出包管理器, 请手动安装 iperf3"; fi
        echo "    ────────────────────────────────────────"
      fi
    fi
    if command -v iperf3 >/dev/null 2>&1; then
      ok "iperf3 $(iperf3 --version 2>/dev/null | awk 'NR==1{print $2}') 已就绪"
    else
      # 不中止 —— 基础调优(BBR/缓冲区/起步)完全不依赖 iperf3, 那也是收益最大的一部分.
      # 少掉的是: 实测带宽、扫拐点、验证吞吐.
      HAVE_IPERF3=0; QN=2
      explain_pkg_failure
      warn "所以这次只能做基础调优:"
      warn "  不能实测带宽(要你手填)、不能扫限速器拐点、不能验证吞吐."
      warn "  基础调优本身照做, 那是收益最大的一部分."
      warn "  装上 iperf3 之后重跑一次 $(disp), 就能补上剩下的."
    fi
  fi

  # ping 单独检查, 【不能】嵌进上面 iperf3 的 else 分支里 ——
  # 实测有机器装了 iperf3 却没有 ping(Ubuntu 22.04 精简镜像), 那样会走进
  # "iperf3 已经安装" 那一支, 永远问不到 ping.
  # 缺 ping 的后果: auto_pick_peer 靠它给 18 个节点排延迟, 全拿不到就返回空,
  # 向导最后报 "公共测速服务器暂时都不可用" —— 那句在甩锅给无辜的对端.
  # ping 存在【不代表能用】: GNU inetutils 版不认 -4, 会让自动选对端全军覆没.
  # 这个分支必须在"没有 ping"之前判不了(没装时 ping_variant 返回 none),
  # 所以放在后面, 等 ping 确实存在时再看它是哪一种.
  if [ "$HAVE_IPERF3" = 1 ] && command -v ping >/dev/null 2>&1; then
    check_ping_variant || true
  fi
  if [ "$HAVE_IPERF3" = 1 ] && ! command -v ping >/dev/null 2>&1; then
    echo
    echo "  自动挑选测速对端需要 ping, 本机没有."
    echo
    if confirm "  安装 ping？" y; then
      echo "    ────────────────────────────────────────"
      if   command -v apt-get >/dev/null; then apt-get update -qq && apt-get install -y iputils-ping
      elif command -v dnf     >/dev/null; then dnf install -y iputils
      elif command -v yum     >/dev/null; then yum install -y iputils
      elif command -v apk     >/dev/null; then apk add iputils
      else warn "认不出包管理器, 请手动安装 ping"; fi
      echo "    ────────────────────────────────────────"
    fi
    if command -v ping >/dev/null 2>&1; then
      ok "ping 已就绪"
    else
      warn "本机缺少 ping, 无法自动选择对端."
      warn "  安装 iputils-ping, 或在下一步手动填写对端."
    fi
  fi

  # ── 1/3 带宽 ────────────────────────────────────────────────────────────
  step "1/${QN}  确认带宽"
  echo
  echo "    你这台机器的带宽是多少 Mbps？常见 100 / 200 / 300 / 500 / 1000."
  echo
  if [ "$HAVE_IPERF3" = 1 ]; then
    printf "    %s建议手动输入. %s回车会在执行阶段现场实测一个估值……\n" "$yellow" "$plain"
    echo "    跳过扫描. 填 0 表示不做整形（端口没有限速器时选这个）."
    echo "    已经知道限速值？输入 m 跳过拐点扫描直接指定"
  else
    printf "    %s没有 iperf3, 必须手动填一个数字.%s\n" "$yellow" "$plain"
  fi
  echo
  # MANUAL_RATE 的三种状态：""=正常扫描 / 数字=直接按该值整形 / "off"=完全不整形
  local bw MANUAL_RATE=""
  while true; do
    bw=$(ask "  带宽 Mbps" "")
    case "$bw" in
      "")      [ "$HAVE_IPERF3" = 0 ] && { warn "  没有 iperf3, 无法实测, 请手动填一个数字"; continue; }
               bw=auto; break ;;                       # 回车 → 执行阶段实测
      0)       MANUAL_RATE=off; bw=auto; break ;;      # 0 → 不整形, 带宽仍需实测
      m|M)                                             # m → 跳到限速值那一问
        while true; do
          MANUAL_RATE=$(ask "  限速值 Mbit" "")
          [ -z "$MANUAL_RATE" ] && { warn "  请填一个数字, 0 表示不做整形"; continue; }
          [ "$MANUAL_RATE" = 0 ] && { MANUAL_RATE=off; bw=auto; break; }
          if { [ "$MANUAL_RATE" -gt 0 ] && [ "$MANUAL_RATE" -le 100000 ]; } 2>/dev/null; then
            bw="$MANUAL_RATE"; break                   # 限速值同时作为算 BDP 的带宽基准
          fi
          warn "  请输入一个正整数（单位 Mbit）, 或 0 表示不做整形"
        done
        break ;;
      *)
        { [ "$bw" -gt 0 ] && [ "$bw" -le 100000 ]; } 2>/dev/null && break
        warn "  请输入一个正整数（单位 Mbps）, 或 m / 0" ;;
    esac
  done

  # ── 2/3 对端 ────────────────────────────────────────────────────────────
  # 没有 iperf3 就没有对端可言, 整段跳过, 且强制不做整形
  local peer="(不需要)"
  if [ "$HAVE_IPERF3" = 0 ]; then
    MANUAL_RATE="${MANUAL_RATE:-off}"
  else
  step "2/${QN}  确认测速对端"
  echo
  echo "    拐点扫描需要一台对端机器跑 iperf3 服务端."
  echo
  echo "    A) 直接回车 —— 用公共节点（默认）"
  echo "       由以下厂商免费提供, 测试流量会发往它们："
  echo "           Leaseweb / Clouvider / OVH"
  echo "           完整列表见 iperf3serverlist.net"
  echo
  echo "    B) 用你自己的另一台机器"
  echo "       在那台机器上执行这两条："
  printf "           %sapt install -y iperf3%s    # 装 iperf3；已装过会跳过, 不会重装\n" "$green" "$plain"
  printf "           %siperf3 -s%s                # 启动服务端, 默认监听 5201 端口\n" "$green" "$plain"
  echo "       然后在下面填那台机器的 IP, 例如  1.2.3.4"
  printf "       %s本脚本默认连 5201 端口%s；对端换了端口的话填  IP:端口  形式. \n" "$yellow" "$plain"
  echo "       对端要选离本机近的."
  echo
  while true; do
    peer=$(ask "  对端 IP / 域名（回车=公共节点）" "")
    if [ -z "$peer" ]; then
      # 不要断言"服务器不可用" —— 失败原因也可能在本机(缺 ping、协议族选错、出站被封),
      # auto_pick_peer 已经把真实原因打在上面了, 这里只说结果.
      local picked; picked=$(auto_pick_peer) || die "没能自动选出对端, 在上一步手动填一个" 2
      peer="${picked%:*}"; PEER_PORT="${picked##*:}"
      set_route_target "$peer"      # 出口按这个目标定, 不按主表 default
      break
    fi
    # 拆主机和端口. 不能只按"最后一个冒号"拆 —— IPv6 地址本身满是冒号:
    #   2001:db8::1  会被拆成 主机=2001:db8: 端口=1, 然后拿着错主机错端口继续跑, 静默出错.
    # 五种形式都要认（不能只收带端口的, 界面上就是教用户填 1.2.3.4 这种裸地址）:
    #   1.2.3.4 / example.com        → 默认 5201
    #   1.2.3.4:5202 / host:5202     → 拆
    #   2001:db8::1                  → 裸 v6, 默认 5201
    #   [2001:db8::1]:5202           → 剥方括号再拆
    # 顺序有讲究: [v6]:port 必须排在 [v6] 前面, 裸 v6 (两个以上冒号) 必须排在 host:port 前面.
    PEER_PORT=5201
    case "$peer" in
      \[*\]:*) PEER_PORT="${peer##*]:}"; peer="${peer%%]:*}"; peer="${peer#\[}" ;;
      \[*\])   peer="${peer#\[}"; peer="${peer%\]}" ;;
      *:*:*)   : ;;
      *:*)     PEER_PORT="${peer##*:}"; peer="${peer%:*}" ;;
    esac
    if ! is_posint "$PEER_PORT" 1 65535; then
      warn "端口必须是 1-65535 之间的整数（IPv6 地址请写成 [地址]:端口）"; echo; continue
    fi
    # 手填的对端当场验一次可达性. 打错 IP 的话不该等到执行阶段才发现 ——
    # 那时前面三个问题都白填了, 而且已经改过 sysctl.
    printf '    检查 %s:%s … ' "$peer" "$PEER_PORT" >&2
    if probe_port "$peer" "$PEER_PORT" 6; then
      printf '%s\n' "$(_c '0;32' '可达')" >&2
      set_route_target "$peer"    # 手填这一支也要设, 不能只在自动选那支设
      break
    fi
    printf '%s\n' "$(_c '0;31' '连不上')" >&2
    echo "      常见原因: 对端没在跑 iperf3 -s / 端口填错 / 防火墙挡了 / IP 打错"
    echo "      回车可以改用公共节点."
    echo
  done
  fi

  # ── 3/3 用途 ────────────────────────────────────────────────────────────
  step "${QN}/${QN}  机器用途"
  echo
  echo "    1) 代理 / 加速        并发连接多, 缓冲区取保守值（最常见）"
  echo "    2) 大文件传输 / 备份  少数大流, 缓冲区取激进值"
  echo
  local rc role
  rc=$(ask "  选择" "1")
  case "$rc" in 2) role=bulk ;; *) role=proxy ;; esac

  # ── 确认 ────────────────────────────────────────────────────────────────
  echo
  rule
  echo "  确认"
  echo
  if [ "$bw" = auto ]; then
    _conf "带宽" "自动实测（执行阶段测）"
  elif [ -n "$MANUAL_RATE" ]; then
    _conf "带宽" "${bw} Mbps"                      # 手填时余量无意义, 不显示
  else
    _conf "带宽" "${bw} Mbps        整形安全余量 $(calc_margin "$bw") Mbit"
  fi
  case "$MANUAL_RATE" in
    "")  _conf "整形" "实测拐点后自动决定" ;;
    off) _conf "整形" "不做整形" ;;
    *)   _conf "整形" "${MANUAL_RATE} Mbit" ;;
  esac
  [ "$HAVE_IPERF3" = 1 ] && _conf "对端" "${peer}:${PEER_PORT}"
  _conf "用途" "$([ "$role" = bulk ] && echo '大文件传输 / 备份' || echo '代理 / 加速')"
  if [ "$HAVE_IPERF3" = 1 ]; then _conf "iperf3" "$(iperf3 --version 2>/dev/null | awk 'NR==1{print $2}')"
  else _conf "iperf3" "无, 只做基础调优"; fi
  _conf "安装位置" "$SELF_PATH"
  echo
  if [ -n "$MANUAL_RATE" ]; then _conf "预计耗时" "约 1 分钟"
  else                              _conf "预计耗时" "约 10 分钟"; fi
  # 填 0 / m 时不扫描, 但验证(单流 + 4 流各约 10 秒)照样满速跑, 填 0 还要先测带宽 ——
  # 早期一律写"很少", 10G 口实际 35 GB(复审实测).
  if [ "$MANUAL_RATE" = off ]; then
    _conf "预计流量" "不扫描; 测带宽和验证仍按端口速率满速跑约 30 秒"
  elif [ -n "$MANUAL_RATE" ]; then
    _conf "预计流量" "约 $(LC_ALL=C awk -v r="$MANUAL_RATE" 'BEGIN{printf "%.1f", r*20/8/1024}') GB（不扫描, 只验证）"
  elif [ "$bw" = auto ]; then   _conf "预计流量" "带宽实测后才能估"
  else
    _conf "预计流量" "约 $(estimate_traffic_gb "$bw") GB"
    _conf ""         "先测一档判断有没有限速器, 没有就到此为止"
  fi
  # 2G 以上扫描代价陡增, 且代理场景的实际流量通常远达不到端口上限.
  # 只提醒, 不阻止 —— 用户可能就是要为大流量场景调.
  if [ -z "$MANUAL_RATE" ] && [ "$bw" != auto ] && [ "$bw" -gt 2000 ] 2>/dev/null; then
    echo
    warn "带宽 ${bw} Mbps 超过 2000, 拐点扫描代价很高."
    echo "      代理场景下实际流量通常远达不到这个值, 整形器很可能从不触发."
    echo "      想跳过的话, 重跑时带宽那一问填 0."
  fi
  rule
  confirm "  开始调优？" y || { info "已取消, 未做任何改动"; return 0; }

  # ══ 执行阶段：全自动, 只在流量远超预估时再问一次 ════════════════════════
  traffic_mark
  printf '\n  %s════ Running ═══════════════════════════════════════════%s\n' "$bold" "$plain"

  printf '\n  %s[1/5] Base tuning%s\n' "$bold" "$plain"
  local skip_scan=0
  if [ "$bw" = auto ]; then
    info "Probing bandwidth (4 streams + pacing, ~15s)..."
    bw=$(probe_bandwidth "$peer" "$(detect_iface)") || die "bandwidth probe failed" 2
    ok "Measured ~${bw} Mbps"
    # 确认页上只能写「带宽实测后才能估」, 用户点头的是一个未知数.
    # 实测完必须把流量摆出来, 大的要重新确认: 10G 口一轮两三百 GB(客户实报 400 GB).
    # 扫描里那道「实测超过所填 3 倍」的保护在这里不起作用 —— 所填就是这次实测值.
    # 回车(默认否)就只做基础调优, 不扫也不验证.
    if [ -z "$MANUAL_RATE" ]; then
      local _est; _est=$(estimate_traffic_gb "$bw")
      info "按实测带宽估算, 扫描和验证约 ${_est} GB 流量"
      if awk -v g="$_est" -v t="$TRAFFIC_CONFIRM_GB" 'BEGIN{exit !(g > t)}' &&
         ! confirm "  预计约 ${_est} GB, 继续扫描？" n; then
        skip_scan=1
      fi
    fi
  fi
  # 小带宽 policer 上 initcwnd 32 的首轮突发会直接打穿令牌桶.
  # 三台 10-20M 真机都表现为首秒重传、后续吞吐逐秒下降;
  # 向导对 <=100M 保留内核默认值. 显式 tune 命令的旧行为不变.
  if [ "$bw" -le 100 ] 2>/dev/null; then
    info "Low-bandwidth path: keeping the kernel default initcwnd"
    cmd_tune --role "$role" --bw "$bw" --no-initcwnd || die "base tuning failed"
  else
    cmd_tune --role "$role" --bw "$bw" || die "base tuning failed"
  fi
  ARCH_PEER="$peer"

  # 用户没同意那么多流量: 路径检查、扫描、验证全是满速测试, 一个都不跑.
  if [ "$skip_scan" = 1 ]; then
    info "按你的选择跳过了拐点扫描和验证, 只做了基础调优"
    [ -x "$QDISC_SCRIPT" ] && info "上次的整形保留未动"
    wizard_archive
    wizard_result "$bw" "" "" "" "$ram" "" "" "" 1
    return 0
  fi

  # 这四个必须在所有分支之前声明. set -u 下, 只要有一条路径没赋值,
  # 结尾传给 wizard_result 时就是 unbound variable —— v0.3.8 的"未检测到限速器"
  # 和"填 0 不整形"两条路都踩了这个(GitHub #1 #2).
  local knee="" rate="" margin="" no_knee=""

  # 手动指定了限速值（或选了不整形）→ 路径验证和拐点扫描都没有意义, 直接跳到应用
  if [ -n "$MANUAL_RATE" ]; then
    printf '\n  %s[2/3] Apply shaping%s\n' "$bold" "$plain"
    if [ "$MANUAL_RATE" = off ]; then
      cmd_shape --off
      rate=""
    else
      # 结果页必须按【实际读回值】显示. 早期版本忽略 cmd_shape 的退出码,
      # 直接 rate="$MANUAL_RATE", 于是 tc class 下发失败时底层已经报了
      # "shaping did not take effect", 结果页却写"已应用整形 50 Mbit",
      # 同一轮的存档里反而正确记着 SHAPE_RATE=none —— 三处互相矛盾.
      if cmd_shape --rate "$MANUAL_RATE"; then
        rate="$MANUAL_RATE"
      else
        warn "整形没有生效, 结果页按网卡实况显示"
        rate=$(tc_rate_mbit "$(tc class show dev "$(detect_iface)" 2>/dev/null)")
        WIZARD_FAILED=1
      fi
      info "Cooling down 15s before verification"
      sleep 15
    fi
    printf '\n  %s[3/3] Verify%s\n' "$bold" "$plain"
    command -v iperf3 >/dev/null && verify_measure "$peer" || warn "no iperf3, throughput not verified"
    wizard_archive
    wizard_result "$bw" "$rate" "$knee" "$margin" "$ram"
    # 手填分支也要传播失败 —— 只在自动分支末尾加 return 是不够的
    [ "$WIZARD_FAILED" = 0 ] || return 1
    return 0
  fi

  printf '\n  %s[2/5] Path quality check%s\n' "$bold" "$plain"
  info "Probing at 40% of ${bw} Mbps -- far below any policer."
  echo "    Retransmits at this rate would mean the link itself is lossy."
  local v; v=$(validate_peer "$peer" "$bw" "$(detect_iface)")
  case "$v" in
    clean:*) ok "Path clean (retrans ${v#clean:})" ;;
    # 链路本身丢包只会让拐点读低一点, 数据仍然有效 —— 警告后照跑, 不打断
    dirty:*) warn "Link is lossy (retrans ${v#dirty:}). The knee may read low; sweep continues." ;;
    slow:*)  warn "Peer only reached ${v#slow:} Mbps. Sweep will decide whether to abort." ;;
    *)       warn "Path check failed; continuing anyway." ;;
  esac

  printf '\n  %s[3/5] Policer sweep%s\n' "$bold" "$plain"
  local sweep_rc=0
  # 把用户已经看过的预估带进去: 填了带宽时就是确认页上那个数, 回车实测时是测完后报的那个.
  # 实际要扫的比它大得多(比如填 3000 而端口跑 8100)时, 扫描前还要再问一次.
  cmd_sweep --peer "$peer" --nominal "$bw" --agreed-gb "$(estimate_traffic_gb "$bw")" || sweep_rc=$?
  # rc=3 是"扫完了但没有可用拐点"(没限速器/超上限/超范围), 结果文件是这轮写的, 可以读.
  # 其他非 0 是这轮压根没跑成, 结果文件已被清空, 不要去读.
  [ "$sweep_rc" = 0 ] || [ "$sweep_rc" = 3 ] || warn "sweep failed, shaping skipped"

  local out_of_range="" above_cap="" inconclusive="" cancelled=""
  if { [ "$sweep_rc" = 0 ] || [ "$sweep_rc" = 3 ]; } && [ -f "$STATE_DIR/sweep.result" ]; then
    ARCH_INCLUDE_SWEEP=1
    cancelled=$(awk -F= '/^CANCELLED/{print $2}' "$STATE_DIR/sweep.result")
    # 取消的那轮不算扫描结果, 存档里不写 SWEEP_ 字段(和以前取消时一致)
    [ -n "$cancelled" ] && ARCH_INCLUDE_SWEEP=0
    no_knee=$(awk -F= '/^NO_KNEE/{print $2}' "$STATE_DIR/sweep.result")
    inconclusive=$(awk -F= '/^INCONCLUSIVE/{print $2}' "$STATE_DIR/sweep.result")
    out_of_range=$(awk -F= '/^OUT_OF_RANGE/{print $2}' "$STATE_DIR/sweep.result")
    above_cap=$(awk -F= '/^ABOVE_CAP/{print $2}' "$STATE_DIR/sweep.result")
    knee=$(awk -F= '/^KNEE/{print $2}'      "$STATE_DIR/sweep.result")
    rate=$(awk -F= '/^RECOMMEND/{print $2}' "$STATE_DIR/sweep.result")
    [ -n "$knee" ] && [ -n "$rate" ] && margin=$(( knee - rate ))
  fi

  # sweep 内部用「跑不到标称 70%」判"测不准", 但 --bw auto 时 nominal 就是
  # 这一轮探出来的慢结果, 等于拿自己跟自己比, 判据形同虚设.
  # 这里补第二个参照: 【已有的整形值】—— 它是上一次实测的产物.
  # 这次只测到它的 70% 以下, 不足以推翻它, 更不该据此把它删掉.
  if [ -n "$no_knee" ] && [ -f "$STATE_DIR/sweep.result" ]; then
    local _prev _uns
    _prev=$(tc_rate_mbit "$(tc class show dev "$(detect_iface)" 2>/dev/null)")
    _uns=$(awk -F= '/^UNSHAPED/{print $2}' "$STATE_DIR/sweep.result")
    if [ -n "$_prev" ] && [ -n "$_uns" ] &&
       awk -v u="$_uns" -v p="$_prev" 'BEGIN{exit !(p > 0 && u < p*0.7)}' 2>/dev/null; then
      warn "本次只测到 ${_uns} Mbps, 远低于已有整形 ${_prev} Mbit —— 判不出限速器是否真的消失."
      no_knee=""; inconclusive=1
      # 存档和 sweep.result 也要跟着改 —— 早期版本只改向导的局部变量,
      # 于是屏幕上说"保留旧整形", 落盘的却仍是 NO_KNEE=1, 两处互相矛盾,
      # 下次读这个文件的人(或菜单 3)会按"没有限速器"处理.
      printf 'INCONCLUSIVE=1\nUNSHAPED=%s\nPREV_SHAPE=%s\n' \
        "$_uns" "$_prev" > "$STATE_DIR/sweep.result"
    fi
  fi

  printf '\n  %s[4/5] Apply shaping%s\n' "$bold" "$plain"
  # 上一轮如果应用过整形, sweep 结束时 qdisc_restore 会把那份配置原样装回来.
  # 所以"这次没检测到限速器"必须【主动移除】, 否则网卡上还挂着上次的限速值,
  # 而结果页写着"整形 未设置" —— 屏幕和实际不一致.
  # 只在 no_knee(确信没有限速器) 时移除; 扫描失败/超范围时并不知道有没有限速器,
  # 保留上次的配置更安全, 但要说清楚.
  if [ -n "$rate" ]; then
    if ! cmd_shape --rate "$rate"; then
      warn "整形没有生效, 结果页按网卡实况显示"
      rate=$(tc_rate_mbit "$(tc class show dev "$(detect_iface)" 2>/dev/null)")
      WIZARD_FAILED=1
    fi
  elif [ -n "$out_of_range" ]; then
    info "policer present but knee not located in range, shaping skipped"
    [ -x "$QDISC_SCRIPT" ] && warn "上次的整形保留未动（本次没测准）"
  elif [ -n "$inconclusive" ]; then
    info "测速能力不足, 判不出有没有限速器, 不改整形"
    [ -x "$QDISC_SCRIPT" ] && warn "上次的整形保留未动（本次没测准, 换个对端再来）"
  elif [ -n "$above_cap" ]; then
    info "unshaped throughput exceeds the sweep cap, shaping skipped"
    if [ -x "$QDISC_SCRIPT" ]; then
      warn "超过扫描上限, 无法判断限速器；上次的整形保留未动"
    else
      info "当前没有旧整形, 网卡立即切换为纯 fq"
      cmd_shape --off
    fi
  elif [ -n "$no_knee" ]; then
    if [ -x "$QDISC_SCRIPT" ]; then
      info "本次未发现限速器, 正在移除上次的整形"
    else
      info "未发现限速器, 当前网卡立即切换为纯 fq"
    fi
    cmd_shape --off
  elif [ -n "$cancelled" ]; then
    info "按你的选择跳过了拐点扫描, 不改整形"
    [ -x "$QDISC_SCRIPT" ] && info "上次的整形保留未动"
  else
    warn "no knee measured, shaping skipped"
    [ -x "$QDISC_SCRIPT" ] && warn "上次的整形保留未动（本次没测出结果）"
  fi

  printf '\n  %s[5/5] Verify%s\n' "$bold" "$plain"
  # 扫描的丢包档会耗尽服务商 policer 的令牌. 立即验证时第一条流会
  # 带着 1%-3% 的残余重传, 而几秒后的第二条流是 0. 和扫描前一样等待
  # 15s, 让验证测的是最终整形效果, 不是上一档的残留状态.
  if [ -n "$cancelled" ]; then
    # 验证同样是满速单流 + 4 流, 10G 机器上又是 20 GB —— 用户刚拒绝过这种流量
    info "验证同样要满速跑, 一并跳过"
  else
    if [ -n "$rate" ] || [ -n "$out_of_range" ] || [ -n "$above_cap" ]; then
      info "Cooling down 15s before verification"
      sleep 15
    fi
    command -v iperf3 >/dev/null && verify_measure "$peer" || warn "no iperf3, throughput not verified"
  fi

  wizard_archive
  wizard_result "$bw" "$rate" "$knee" "$margin" "$ram" "$no_knee" "$out_of_range" "$above_cap" "$cancelled"
  [ "$WIZARD_FAILED" = 0 ] || return 1
}

# 两条向导路径都在最终整形和验证后保存; archive_write 从机器读取实际状态.
# 未扫描或扫描失败时 ARCH_INCLUDE_SWEEP=0, 不把旧扫描写成本轮结果.
wizard_archive(){
  archive_save "wizard-${ARCH_BW}M-rtt${ARCH_RTT}" >/dev/null 2>&1 ||
    warn "调优已生效, 但存档没建成 —— rollback 仍可用($(disp) rollback), 存档功能可稍后手动 $(disp) archive save"
}

# 结果段落. 正常流程和"手动指定整形值"两条路径共用, 避免两份重复的排版代码.
wizard_result(){   # wizard_result <带宽> <整形值> <拐点> <余量> <内存MB> [无拐点] [超范围] [超上限] [跳过扫描]
  local bw="${1:-}" rate="${2:-}" knee="${3:-}" margin="${4:-}" ram="${5:-0}" no_knee="${6:-}" oor="${7:-}" cap="${8:-}"
  local skipped="${9:-}"
  local cur_shape=""
  printf '\n  %s════ 结果 ══════════════════════════════════════════════%s\n' "$bold" "$plain"
  echo
  if [ -n "$knee" ]; then
    _conf "实测端口上限" "${knee} Mbit"
    _conf "安全余量"     "${margin} Mbit（按 ${bw}M 档位）"
    _conf "已应用整形"   "${rate} Mbit"
    echo
  elif [ -n "$rate" ]; then
    _conf "已应用整形"   "${rate} Mbit"
    echo
  else
    # 这几支都是"本次没应用整形". 但网卡上可能还挂着【上一轮】的限速 ——
    # 早期版本一律打"未设置", 屏幕和实际不一致. 这里直接读当前状态再报.
    cur_shape=$(tc_rate_mbit "$(tc class show dev "$(detect_iface)" 2>/dev/null)")
    if [ -n "$cur_shape" ]; then
      _conf "整形"       "${cur_shape} Mbit（上次的配置, 本次未改动）"
    else
      _conf "整形"       "未设置"
    fi
    if   [ -n "$oor" ];     then _conf "原因" "检测到限速迹象, 但未在扫描范围内定位到拐点"
    elif [ -n "$cap" ];     then
      _conf "原因" "不限速吞吐超过 ${cap} Mbit 扫描上限"
      _conf ""     "未判断是否有限速器, 不自动改整形"
    elif [ -n "$no_knee" ]; then _conf "原因" "扫描未发现限速器, 加整形只会限制自己"
    elif [ -n "$skipped" ]; then _conf "原因" "流量太大, 按你的选择跳过了拐点扫描"
    fi
    echo
  fi
  # verify 的判定也要按实况: 本次没应用但网卡上还有旧整形时, 目标值取那个,
  # 否则会给一台正被限速的机器说"这台没有应用整形".
  # 跳过扫描时验证也没跑, 照常打表格会显示两行"测试失败".
  if [ -n "$skipped" ]; then
    echo "  验证"
    echo "      已跳过（和扫描一样要满速跑）"
    echo
  else
    verify_verdict "${rate:-$cur_shape}"
  fi
  traffic_report
  echo
  echo "  本次改动和快照位置"
  echo "      $SYSCTL_FILE"
  [ -n "$rate" ] && echo "      $QDISC_UNIT"
  echo "      $SNAPSHOT"

  # 小内存且没 swap 才提. 内存够用或已有 swap 就完全不出现这一段.
  if [ "$ram" -le 1024 ] && ! not_blank "$(swapon --show 2>/dev/null)"; then
    step "swap"
    echo
    echo "    本机 ${ram} MB 内存且没有 swap. 跑代理时 TCP 缓冲区可能撑爆内存,"
    echo "    代理进程被系统杀掉."
    echo
    echo "    输入 1-20 的数字（单位 GB）, 推荐 1-4；回车 = 2；输入 0 = 不创建."
    echo
    # 这里必须自己校验. 直接把输入丢给 cmd_harden 的话, 非法值会触发它的 die,
    # 整个脚本跟着退出, 连"调优完成"都打不出来 —— v0.4.3 就是这么挂的.
    local sg
    while true; do
      sg=$(ask "  swap 大小 GB" "2")
      [ "$sg" = 0 ] && break
      if is_posint "${sg%[Gg]}" 1 20; then cmd_harden --swap "$sg"; break; fi
      warn "  请输入 1-20 之间的整数, 或 0 跳过"
    done
  fi
  echo
  # 整形下发失败时不能说"完成"、也不能返回 0 —— 上层脚本和监控看退出码.
  # 数值已经按实况显示了(D04), 但状态传播是另一件事.
  if [ "${WIZARD_FAILED:-0}" = 1 ]; then
    warn "调优未完全成功: 整形没有生效, 上面的结果页已按网卡实况显示."
  else
    ok "调优完成."
  fi
  echo
  echo "  ─────────────────────────────────────────────"
  echo "    菲比VPS补货频道：        t.me/vpskuaibu"
  echo "    星空VPS（免费API查阅机型）： spacevps.cc"
  echo "    问题反馈：               github.com/Kylin010/tcpfit/issues"
  echo "    合作：                   4496540pva@gmail.com"
  echo
}

menu_loop(){
  need_root
  # 菜单和一键调优全靠问答. 没有终端时 ask 只能拿默认值, 等于每一问都按默认同意:
  # 实测 `ssh host tcpfit`(没带 -t) / cron / 面板「执行命令」会自动选 1、实测带宽、
  # 选公共节点、「开始调优」, 一路跑完扫描和整形 —— 10G 机器上是两三百 GB.
  # 没装上 iperf3 时还会在「请手动填一个数字」那里死循环刷屏(25 秒 1.5 万行).
  # 放在 self_install / telemetry 之前: 什么都没做的一次不该装文件、也不该计数.
  if ! have_tty; then
    warn "没有可交互的终端. 菜单和一键调优要回答问题, 不能无人值守运行."
    # 还没装过(第一次用 bash <(curl …) 跑)时, /usr/local/bin/tcpfit 不存在, 不能拿它举例
    if [ -x "$SELF_PATH" ]; then
      echo "  远程执行请加 -t:   ssh -t <主机> tcpfit"
      echo "  脚本里请用子命令:  tcpfit tune --bw 500   （tcpfit help 看全部）"
    else
      echo "  远程执行请加 -t:   ssh -t <主机> 'bash <(curl -fsSL $SELF_URL)'"
      echo "  脚本里请用子命令:  bash <(curl -fsSL $SELF_URL) tune --bw 500"
    fi
    exit 1
  fi
  take_lock
  migrate_legacy
  self_install
  telemetry_ping
  while true; do
    banner
    echo
    local c; c=$(ask "  请选择 / Select [0-9,u]" "1")
    echo
    case "$c" in
      1) wizard
         # 跑完直接退出, 不回菜单. 回菜单要经过 banner 的 clear, 而 clear 发的是
         # \033[H\033[2J\033[3J —— 那个 3J 连滚动回滚缓冲一起清掉, 往上翻也找不回
         # 结果. 调优要跑十几分钟, 结果页面就是用户唯一要看的东西, 不能这么洗掉.
         echo
         echo "  要继续操作, 重新运行 ${bold}tcpfit${plain}"
         echo
         # 退出前必须清掉. 这十几分钟里用户随手按的键还躺在 tty 缓冲里,
         # 进程一退, 它们就被父 shell 读走当命令执行（实测 ls -la / whoami 真的跑了）.
         drain_tty
         exit 0 ;;
      2) local r; r=$(ask "  用途 1) 代理/加速  2) 大文件传输" "1")
         local role=proxy; [ "$r" = 2 ] && role=bulk
         local b; b=$(ask "  带宽 Mbps (回车=自动探测)" "")
         if [ -n "$b" ]; then cmd_tune --role "$role" --bw "$b"
         else
           local p; if p=$(auto_pick_peer); then PEER_PORT="${p##*:}"; cmd_tune --role "$role" --bw auto --peer "${p%:*}"
           else warn "No peer available; specify bandwidth manually"; fi
         fi ;;
      3) local p; if p=$(auto_pick_peer); then
           PEER_PORT="${p##*:}"; p="${p%:*}"
           local b; b=$(ask "  带宽 Mbps" "")
           cmd_sweep --peer "$p" --nominal "$b"
           local rate; rate=$(awk -F= '/^RECOMMEND/{print $2}' "$STATE_DIR/sweep.result" 2>/dev/null)
           [ -n "$rate" ] && confirm "  应用 ${rate}Mbit 整形？" y && cmd_shape --rate "$rate"
         else warn "No peer available"; fi ;;
      4) if not_blank "$(swapon --show 2>/dev/null)"; then
           info "已有 swap: $(free -h | awk '/Swap/{print $2}'), 回车跳过；要再建就输入数字"
           local sg; sg=$(ask "  swap 大小 GB (1-20, 回车跳过)" ""); [ -n "$sg" ] && cmd_harden --swap "$sg"
         else
           echo "  输入 1-20 的数字（单位 GB）, 推荐 1-4；回车 = 2；输入 0 = 不创建."
           local sg; sg=$(ask "  swap 大小 GB" "2"); [ "$sg" != 0 ] && cmd_harden --swap "$sg"
         fi ;;
      5) cmd_status ;;
      6) local p; if p=$(auto_pick_peer); then PEER_PORT="${p##*:}"; cmd_verify --peer "${p%:*}"; else cmd_verify; fi ;;
      7) confirm "  确定回滚全部改动？" && cmd_rollback ;;
      8) cmd_update --from-menu ;;
      9) echo; archive_list; echo
         local a; a=$(ask "  回滚到哪个存档？(序号, 回车跳过)" "")
         if [ -n "$a" ]; then
           local f
           if f=$(archive_find "$a"); then
             confirm "  确定回滚到 $(archive_seq_of "$f") $(archive_name_of "$f")？" && archive_restore "$f"
           else
             local find_rc=$?
             [ "$find_rc" = 2 ] || warn "找不到存档: $a"
           fi
         fi ;;
      u|U) cmd_uninstall; exit $? ;;
      0) exit 0 ;;
      *) warn "Invalid selection" ;;
    esac
    echo
    drain_tty
    printf "  ${yellow}按任意键返回${plain}"
    read -rsn1 2>/dev/null </dev/tty || read -r 2>/dev/null </dev/tty || true
    echo
  done
}

# ── 入口 ────────────────────────────────────────────────────────────────────
usage(){ awk 'NR==1 {next} /^#/ {sub(/^# ?/, ""); print; next} {exit}' "$0"; }

# FLYX: library only; the upstream interactive/CLI entry point is omitted.

