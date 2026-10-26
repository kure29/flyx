import { useEffect, useRef, useState } from "react";
import toast from "react-hot-toast";

import { Button } from "@/shadcn-bridge/heroui/button";
import { Checkbox } from "@/shadcn-bridge/heroui/checkbox";
import { Chip } from "@/shadcn-bridge/heroui/chip";
import { Progress } from "@/shadcn-bridge/heroui/progress";
import { Spinner } from "@/shadcn-bridge/heroui/spinner";
import {
  Modal,
  ModalContent,
  ModalHeader,
  ModalBody,
  ModalFooter,
} from "@/shadcn-bridge/heroui/modal";
import { nodeTune, type NodeTuneAction, type NodeTuneStatus } from "@/api";

const mb = (bytes: number) => `${Math.round((bytes / 1048576) * 10) / 10} MiB`;

export function NodeTuneModal({
  node,
  onClose,
}: {
  node: { id: number; name: string } | null;
  onClose: () => void;
}) {
  const [status, setStatus] = useState<NodeTuneStatus | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState<NodeTuneAction | "">("");
  const [detail, setDetail] = useState<string | null>(null);
  const [showDeep, setShowDeep] = useState(false);
  const [allowTraffic, setAllowTraffic] = useState(false);
  const generation = useRef(0);
  const lastJob = useRef<NodeTuneStatus["job"]>(undefined);
  const running = status?.job?.state === "running";
  const detection = status?.detection;
  const hasRecentMeasurement = Boolean(
    detection?.applied &&
      Date.now() - detection.measuredAt < 24 * 60 * 60 * 1000,
  );
  const hasTrafficEstimate = Boolean(
    detection &&
      Number.isFinite(detection.estimatedGB) &&
      detection.estimatedGB > 0,
  );
  const trafficBudget =
    hasTrafficEstimate && detection
      ? Math.min(1000, Math.ceil(detection.estimatedGB * 1.25 * 10) / 10)
      : 0;
  const deepAvailable = Boolean(
    status?.automatic &&
      hasRecentMeasurement &&
      hasTrafficEstimate &&
      detection &&
      detection.estimatedGB <= 1000,
  );
  const needsMeasurement = !hasRecentMeasurement || !hasTrafficEstimate;
  const deepPreparationMessage = !status?.automatic
    ? "这台服务器的 Agent 需要升级。请关闭窗口，在服务器操作菜单中升级 Agent，再重新打开调优。"
    : !detection?.applied
      ? "还没有自动测速结果。先完成自动调优，再确认流量开始深度扫描。"
      : !hasRecentMeasurement
        ? "上次测速已超过 24 小时，需要先重新自动调优，再确认流量开始深度扫描。"
        : !hasTrafficEstimate
          ? "上次测速缺少有效的流量估算，需要先重新自动调优。"
          : `本次预计流量约 ${detection.estimatedGB.toFixed(1)} GB，超过 1000 GB 上限，暂不能开始深度扫描。`;

  useEffect(() => {
    const current = ++generation.current;

    setStatus(null);
    setError("");
    setBusy("");
    setDetail(null);
    setShowDeep(false);
    setAllowTraffic(false);
    lastJob.current = undefined;
    if (!node) return;
    void nodeTune(node.id, "status")
      .then((res) => {
        if (generation.current !== current) return;
        if (res.code !== 0 || !res.data) {
          setError(res.msg || "读取服务器状态失败");

          return;
        }
        setStatus(res.data);
      })
      .catch(() => {
        if (generation.current === current) setError("读取服务器状态失败");
      });

    return () => {
      generation.current++;
    };
  }, [node?.id]);

  useEffect(() => {
    if (!node || !running) return;
    const current = generation.current;
    let pending = false;
    const timer = window.setInterval(() => {
      if (pending) return;
      pending = true;
      void nodeTune(node.id, "status")
        .then((res) => {
          if (generation.current !== current) return;
          if (res.code === 0 && res.data) {
            setStatus(res.data);
            setError("");
          } else setError(res.msg || "暂时无法读取进度，正在重试");
        })
        .catch(() => {
          if (generation.current === current)
            setError("暂时无法读取进度，正在重试；任务仍在服务器运行");
        })
        .finally(() => {
          pending = false;
        });
    }, 2000);

    return () => window.clearInterval(timer);
  }, [node?.id, running]);

  useEffect(() => {
    const job = status?.job;
    const previous = lastJob.current;

    if (
      job &&
      previous?.id === job.id &&
      previous.state === "running" &&
      job.state !== "running"
    ) {
      if (job.state === "succeeded") toast.success(job.message);
      else if (job.state === "failed") toast.error(job.message);
      else toast(job.message);
    }
    lastJob.current = job;
  }, [status?.job]);

  const run = async (action: NodeTuneAction, continueToDeep = false) => {
    if (!node) return;
    const current = generation.current;

    setBusy(action);
    setError("");
    try {
      const res = await nodeTune(
        node.id,
        action,
        action === "deep" ? { maxTrafficGB: trafficBudget } : {},
      );

      if (generation.current !== current) return;
      if (res.code !== 0 || !res.data) {
        const message = res.msg || "调优请求失败";

        setError(message);
        toast.error(message);

        return;
      }
      setStatus(res.data);
      setShowDeep(continueToDeep);
      setAllowTraffic(false);
    } catch {
      if (generation.current === current)
        setError("请求未完成，任务可能已开始；重新打开可查看服务器进度");
    } finally {
      if (generation.current === current) setBusy("");
    }
  };

  return (
    <Modal
      backdrop="blur"
      classNames={{
        base: "!w-[calc(100%-32px)] !mx-auto sm:!w-full rounded-2xl overflow-hidden",
      }}
      isOpen={Boolean(node)}
      placement="center"
      scrollBehavior="inside"
      size="2xl"
      onOpenChange={(open) => !open && onClose()}
    >
      <ModalContent>
        <ModalHeader className="flex flex-col gap-1">
          <h2 className="text-xl font-bold">服务器调优</h2>
          <p className="text-sm font-normal text-default-500">
            {node?.name}
            {status &&
              ` · 内核 ${status.kernel || "未知"} · 拥塞控制 ${status.congestion || "未知"}`}
          </p>
        </ModalHeader>
        <ModalBody>
          {error && (
            <p className="text-sm text-danger" role="alert">
              {error}
            </p>
          )}
          {!status ? (
            !error && (
              <div className="flex justify-center py-10">
                <Spinner />
              </div>
            )
          ) : (
            <div className="space-y-3">
              <p className="text-xs text-default-500">
                自动实测带宽，结合内存计算参数，无需填写带宽和延迟。首次会安装所需的测速依赖。测速流量发往公共对端，千兆线路基础测速约消耗
                4 GB。
              </p>
              {!status.automatic && !showDeep && (
                <p className="rounded-xl bg-warning/10 p-3 text-sm text-warning-800 dark:text-warning-200">
                  这台服务器的 Agent 不支持自动调优，请先升级 Agent。
                </p>
              )}
              {status.job && (
                <div
                  aria-live="polite"
                  className="space-y-2 rounded-xl border border-divider p-3"
                >
                  <p
                    className={
                      status.job.state === "failed"
                        ? "text-sm text-danger"
                        : "text-sm"
                    }
                  >
                    {status.job.message}
                  </p>
                  <Progress
                    aria-label="调优进度"
                    color={status.job.state === "failed" ? "danger" : "primary"}
                    size="sm"
                    value={status.job.percent}
                  />
                  {running && (
                    <p className="text-xs text-default-500">
                      关闭窗口后任务继续运行，重新打开可查看进度。
                    </p>
                  )}
                  {running && showDeep && status.job.mode === "auto" && (
                    <p className="text-xs text-default-500">
                      正在完成第一步自动调优；完成后会显示流量确认，不会直接开始深度扫描。
                    </p>
                  )}
                  {status.job.logs && (
                    <details className="text-xs text-default-500">
                      <summary className="cursor-pointer">查看任务日志</summary>
                      <pre className="mt-2 max-h-48 overflow-auto whitespace-pre-wrap break-all">
                        {status.job.logs}
                      </pre>
                    </details>
                  )}
                </div>
              )}
              {detection && (
                <div className="space-y-2 rounded-xl border border-divider p-3 text-xs">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="text-sm font-medium">
                      实测带宽 {detection.bandwidthMbps} Mbps
                    </span>
                    <Chip size="sm" variant="flat">
                      {detection.bandwidthSource}
                    </Chip>
                  </div>
                  <p className="text-default-500">
                    出口 {detection.interface} · IPv{detection.family} · 对端{" "}
                    {detection.peer}:{detection.port}
                  </p>
                  <p className="text-default-500">{detection.rttSource}</p>
                  <p className="text-default-500">
                    测速时间 {new Date(detection.measuredAt).toLocaleString()}
                  </p>
                  {status.sizes && (
                    <p>
                      内存 {status.sizes.ramMb} MiB · 缓冲区上限{" "}
                      {mb(status.sizes.bufMax)}（{status.sizes.bufReason}）
                    </p>
                  )}
                  <div className="grid grid-cols-3 gap-2">
                    {(
                      [
                        ["调优前多连接", detection.before],
                        ["调优后单连接", detection.single],
                        ["调优后多连接", detection.parallel],
                      ] as const
                    ).map(([label, sample]) => (
                      <div
                        key={label}
                        className="rounded-lg bg-default-100 p-2"
                      >
                        <p className="text-default-500">{label}</p>
                        <p className="mt-1 font-medium">
                          {sample ? `${sample.mbps.toFixed(1)} Mbps` : "待验证"}
                        </p>
                        {sample && (
                          <p className="mt-1 text-[11px] text-default-500">
                            重传 {sample.retransmits} ·{" "}
                            {sample.lossPercent.toFixed(3)}%
                          </p>
                        )}
                      </div>
                    ))}
                  </div>
                  {detection.deepSummary && <p>{detection.deepSummary}</p>}
                  {detection.warnings?.map((warning) => (
                    <p
                      key={warning}
                      className="text-warning-700 dark:text-warning-300"
                    >
                      {warning}
                    </p>
                  ))}
                </div>
              )}
              {showDeep && !running && (
                <div
                  aria-label="深度优化准备"
                  className="space-y-3 rounded-xl border border-warning/25 bg-warning/10 p-3 text-xs text-warning-800 dark:text-warning-200"
                >
                  <p className="font-medium">
                    深度优化：扫描限速器拐点，按结果决定是否应用出向整形。
                  </p>
                  {deepAvailable && detection ? (
                    <>
                      <p>
                        预计流量约 {detection.estimatedGB.toFixed(1)}{" "}
                        GB，本次预算 {trafficBudget.toFixed(1)}{" "}
                        GB，达到预算会停止。过程可能需要数分钟，线路业务流量也会计入预算。
                      </p>
                      <Checkbox
                        isSelected={allowTraffic}
                        size="sm"
                        onValueChange={setAllowTraffic}
                      >
                        允许本次测速使用上述流量
                      </Checkbox>
                      <Button
                        color="warning"
                        isDisabled={!allowTraffic || running || busy !== ""}
                        size="sm"
                        onPress={() => void run("deep")}
                      >
                        开始深度优化
                      </Button>
                    </>
                  ) : (
                    <>
                      <p role="status">{deepPreparationMessage}</p>
                      {status.automatic && needsMeasurement && (
                        <Button
                          color="primary"
                          isDisabled={busy !== ""}
                          isLoading={busy === "auto"}
                          size="sm"
                          onPress={() => void run("auto", true)}
                        >
                          自动调优并继续
                        </Button>
                      )}
                    </>
                  )}
                  <Button
                    isDisabled={busy !== ""}
                    size="sm"
                    variant="light"
                    onPress={() => {
                      setShowDeep(false);
                      setAllowTraffic(false);
                    }}
                  >
                    关闭引导
                  </Button>
                </div>
              )}
              <p className="text-xs text-default-500">
                参数应用后立即生效并保存；恢复原配置会撤销 FLYX 的调优和整形。
              </p>
              {status.groups.map((group) => (
                <details
                  key={group.id}
                  className="rounded-xl border border-divider px-3 py-2.5"
                  open={detail === group.id}
                >
                  <summary
                    className="flex cursor-pointer items-center justify-between gap-2 text-sm"
                    onClick={(event) => {
                      event.preventDefault();
                      setDetail(detail === group.id ? null : group.id);
                    }}
                  >
                    {group.name}
                    <Chip
                      color={group.applied ? "success" : "default"}
                      size="sm"
                      variant="flat"
                    >
                      {group.applied ? "已满足" : "查看参数"}
                    </Chip>
                  </summary>
                  <p className="mt-2 text-xs text-default-500">{group.desc}</p>
                  <div className="mt-2 space-y-1 font-mono text-[11px] text-default-600">
                    {group.settings.map((setting) => (
                      <div key={setting.key} className="break-all">
                        {setting.key}:{" "}
                        {setting.supported ? setting.current : "不支持"}
                        {detection &&
                          setting.supported &&
                          !setting.ok &&
                          ` → ${setting.target}`}
                      </div>
                    ))}
                  </div>
                </details>
              ))}
            </div>
          )}
        </ModalBody>
        <ModalFooter className="flex-wrap gap-2">
          {(status?.hasBackup || detection) && (
            <Button
              className="mr-auto"
              isDisabled={running || busy !== ""}
              variant="light"
              onPress={() =>
                void run(status?.automatic ? "restoreAuto" : "restore")
              }
            >
              恢复原配置
            </Button>
          )}
          {/* Closing leaves a running job going; it shows again on reopen. */}
          <Button variant="light" onPress={onClose}>
            关闭
          </Button>
          {running ? (
            <Button
              color="warning"
              isDisabled={busy !== "" || status?.job?.mode === "restore"}
              onPress={() => void run("cancel")}
            >
              停止任务
            </Button>
          ) : (
            <>
              <Button
                isDisabled={!status || busy !== ""}
                variant="bordered"
                onPress={() => {
                  setShowDeep(!showDeep);
                  setAllowTraffic(false);
                }}
              >
                深度优化
              </Button>
              <Button
                color="primary"
                isDisabled={!status?.automatic || busy !== ""}
                isLoading={busy === "auto"}
                onPress={() => void run("auto")}
              >
                自动调优
              </Button>
            </>
          )}
        </ModalFooter>
      </ModalContent>
    </Modal>
  );
}
