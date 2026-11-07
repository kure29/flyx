#!/usr/bin/env bash
# FLYX's noninteractive adapter. See README.md and LICENSE.
source "$(dirname "$0")/tcpfit.sh"
STATE_DIR="$1"; action="$2"; iface="$3"; peer="$4"; PEER_PORT="$5"; IP_FAMILY="$6"
shift 6
QDISC_SCRIPT="$STATE_DIR/shape.sh"
WIZARD=1
confirm(){ return 1; }
take_lock(){ :; } # The agent serializes jobs, including legacy TuneApply.
take_snapshot(){ :; } # The agent persists both sysctl and queue originals first.
detect_iface(){ printf '%s\n' "$iface"; }
reap_iperf(){ :; } # CommandContext owns and terminates the entire process tree.
qdisc_guard(){ return 0; } # Validated and captured by the agent before sweep.
qdisc_save(){ QSAVE_IFACE="$1"; }
qdisc_restore(){ bash "$STATE_DIR/current-qdisc.sh"; }
case "$action" in
  sample)
    value=$(run_iperf "$peer" 10 "$1")
    [ -n "$value" ] || die "iperf3 测速失败：公共对端忙或不可达，请稍后重试" 2
    printf 'FLYX_SAMPLE=%s\n' "$value"
    ;;
  sweep)
    trap 'qdisc_restore' EXIT
    rc=0
    cmd_sweep --peer "$peer" --nominal "$1" --agreed-gb "$2" || rc=$?
    # Some upstream paths install a narrower signal trap. EXIT always replays
    # the captured queues, including when the upstream code calls die.
    qdisc_restore || exit 1
    if [ -f "$STATE_DIR/sweep.result" ]; then
      sed 's/^/FLYX_SWEEP_/' "$STATE_DIR/sweep.result"
    fi
    exit "$rc"
    ;;
  shape)
    apply_test_shaper "$iface" "$1" || die "出向整形应用失败"
    actual=$(tc_rate_mbit "$(tc class show dev "$iface")")
    [ "$actual" = "$1" ] || die "出向整形校验失败"
    ;;
  *) die "未知 FLYX 调优操作" ;;
esac
