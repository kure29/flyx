import type { Dispatch, SetStateAction } from "react";

import { Button } from "@/shadcn-bridge/heroui/button";
import { Textarea } from "@/shadcn-bridge/heroui/input";
import { Select, SelectItem } from "@/shadcn-bridge/heroui/select";
import {
  Modal,
  ModalContent,
  ModalHeader,
  ModalBody,
  ModalFooter,
} from "@/shadcn-bridge/heroui/modal";
import { ImportFormat, Tunnel } from "@/pages/forward/types";

export interface ExportModalProps {
  copyExportData: () => Promise<void>;
  executeExport: () => void;
  exportData: string;
  exportLoading: boolean;
  exportModalOpen: boolean;
  selectedTunnelForExport: number | null;
  setExportData: Dispatch<SetStateAction<string>>;
  setExportModalOpen: Dispatch<SetStateAction<boolean>>;
  setSelectedTunnelForExport: Dispatch<SetStateAction<number | null>>;
  tunnels: Tunnel[];
}

/** Export forwards as text. */
export function ExportModal({
  copyExportData,
  executeExport,
  exportData,
  exportLoading,
  exportModalOpen,
  selectedTunnelForExport,
  setExportData,
  setExportModalOpen,
  setSelectedTunnelForExport,
  tunnels,
}: ExportModalProps) {
  return (
    <Modal
      backdrop="blur"
      classNames={{
        base: "!w-[calc(100%-32px)] !mx-auto sm:!w-full rounded-2xl overflow-hidden",
      }}
      isOpen={exportModalOpen}
      placement="center"
      scrollBehavior="inside"
      size="2xl"
      onClose={() => {
        setExportModalOpen(false);
        setSelectedTunnelForExport(null);
        setExportData("");
      }}
    >
      <ModalContent>
        <ModalHeader closable className="flex flex-col gap-1">
          <h2 className="text-xl font-bold">导出规则数据</h2>
          <p className="text-small text-default-500">
            格式：目标地址|规则名称|入口端口
          </p>
        </ModalHeader>
        <ModalBody className="pb-6">
          <div className="space-y-4">
            {/* 隧道选择 */}
            <div>
              <Select
                isRequired
                label="选择导出隧道"
                placeholder="请选择要导出的隧道"
                selectedKeys={
                  selectedTunnelForExport
                    ? [selectedTunnelForExport.toString()]
                    : []
                }
                variant="bordered"
                onSelectionChange={(keys) => {
                  const selectedKey = Array.from(keys)[0] as string;

                  setSelectedTunnelForExport(
                    selectedKey ? parseInt(selectedKey) : null,
                  );
                }}
              >
                {tunnels.map((tunnel) => (
                  <SelectItem
                    key={tunnel.id.toString()}
                    textValue={tunnel.name}
                  >
                    {tunnel.name}
                  </SelectItem>
                ))}
              </Select>
            </div>

            {/* 导出按钮和数据 */}
            {exportData && (
              <div className="flex justify-between items-center">
                <Button
                  color="primary"
                  isDisabled={!selectedTunnelForExport}
                  isLoading={exportLoading}
                  size="sm"
                  startContent={
                    <svg
                      aria-hidden="true"
                      className="w-4 h-4"
                      fill="currentColor"
                      viewBox="0 0 20 20"
                    >
                      <path
                        clipRule="evenodd"
                        d="M3 17a1 1 0 011-1h12a1 1 0 110 2H4a1 1 0 01-1-1zM6.293 6.707a1 1 0 010-1.414l3-3a1 1 0 011.414 0l3 3a1 1 0 01-1.414 1.414L11 5.414V13a1 1 0 11-2 0V5.414L7.707 6.707a1 1 0 01-1.414 0z"
                        fillRule="evenodd"
                      />
                    </svg>
                  }
                  onPress={executeExport}
                >
                  重新生成
                </Button>
                <Button
                  color="secondary"
                  size="sm"
                  startContent={
                    <svg
                      aria-hidden="true"
                      className="w-4 h-4"
                      fill="currentColor"
                      viewBox="0 0 20 20"
                    >
                      <path d="M8 3a1 1 0 011-1h2a1 1 0 110 2H9a1 1 0 01-1-1z" />
                      <path d="M6 3a2 2 0 00-2 2v11a2 2 0 002 2h8a2 2 0 002-2V5a2 2 0 00-2-2 3 3 0 01-3 3H9a3 3 0 01-3-3z" />
                    </svg>
                  }
                  onPress={copyExportData}
                >
                  复制
                </Button>
              </div>
            )}

            {/* 初始导出按钮 */}
            {!exportData && (
              <div className="text-right">
                <Button
                  color="primary"
                  isDisabled={!selectedTunnelForExport}
                  isLoading={exportLoading}
                  size="sm"
                  startContent={
                    <svg
                      aria-hidden="true"
                      className="w-4 h-4"
                      fill="currentColor"
                      viewBox="0 0 20 20"
                    >
                      <path
                        clipRule="evenodd"
                        d="M3 17a1 1 0 011-1h12a1 1 0 110 2H4a1 1 0 01-1-1zM6.293 6.707a1 1 0 010-1.414l3-3a1 1 0 011.414 0l3 3a1 1 0 01-1.414 1.414L11 5.414V13a1 1 0 11-2 0V5.414L7.707 6.707a1 1 0 01-1.414 0z"
                        fillRule="evenodd"
                      />
                    </svg>
                  }
                  onPress={executeExport}
                >
                  生成导出数据
                </Button>
              </div>
            )}

            {/* 导出数据显示 */}
            {exportData && (
              <div className="relative">
                <Textarea
                  readOnly
                  className="font-mono text-sm"
                  classNames={{
                    input: "font-mono text-sm",
                  }}
                  maxRows={20}
                  minRows={10}
                  placeholder="暂无数据"
                  value={exportData}
                  variant="bordered"
                />
              </div>
            )}
          </div>
        </ModalBody>
      </ModalContent>
    </Modal>
  );
}

export interface ImportModalProps {
  executeImport: () => Promise<void>;
  importData: string;
  importFormat: ImportFormat;
  importLoading: boolean;
  importModalOpen: boolean;
  importResults: {
    line: string;
    success: boolean;
    message: string;
    forwardName?: string;
  }[];
  selectedTunnelForImport: number | null;
  setImportData: Dispatch<SetStateAction<string>>;
  setImportFormat: Dispatch<SetStateAction<ImportFormat>>;
  setImportModalOpen: Dispatch<SetStateAction<boolean>>;
  setImportResults: Dispatch<
    SetStateAction<
      {
        line: string;
        success: boolean;
        message: string;
        forwardName?: string;
      }[]
    >
  >;
  setSelectedTunnelForImport: Dispatch<SetStateAction<number | null>>;
  tunnels: Tunnel[];
}

/** Import forwards from text. */
export function ImportModal({
  executeImport,
  importData,
  importFormat,
  importLoading,
  importModalOpen,
  importResults,
  selectedTunnelForImport,
  setImportData,
  setImportFormat,
  setImportModalOpen,
  setImportResults,
  setSelectedTunnelForImport,
  tunnels,
}: ImportModalProps) {
  return (
    <Modal
      backdrop="blur"
      classNames={{
        base: "!w-[calc(100%-32px)] !mx-auto sm:!w-full rounded-2xl overflow-hidden",
      }}
      isOpen={importModalOpen}
      placement="center"
      scrollBehavior="inside"
      size="2xl"
      onClose={() => setImportModalOpen(false)}
    >
      <ModalContent>
        <ModalHeader className="flex flex-col gap-1">
          <h2 className="text-xl font-bold">导入规则数据</h2>
          {importFormat === "flyx" ? (
            <>
              <p className="text-small text-default-500">
                格式：目标地址|规则名称|入口端口，每行一个，入口端口留空将自动分配可用端口
              </p>
              <p className="text-small text-default-400">
                目标地址支持单个地址(如：example.com:8080)或多个地址用逗号分隔(如：3.3.3.3:3,4.4.4.4:4)
              </p>
            </>
          ) : (
            <>
              <p className="text-small text-default-500">
                ny格式：JSON对象，支持多个目标地址（负载均衡），按所选隧道导入
              </p>
              <p className="text-small text-default-400">
                格式：&#123;&quot;dest&quot;:[&quot;地址:端口&quot;],&quot;listen_port&quot;:端口,&quot;name&quot;:&quot;名称&quot;&#125;（listen_port可省略，自动分配端口）
              </p>
            </>
          )}
        </ModalHeader>
        <ModalBody className="pb-6">
          <div className="space-y-4">
            {/* 格式选择 */}
            <Select
              isRequired
              label="导入格式"
              placeholder="请选择导入格式"
              selectedKeys={[importFormat]}
              variant="bordered"
              onSelectionChange={(keys) => {
                const selectedKey = Array.from(keys)[0] as ImportFormat;

                if (selectedKey) {
                  setImportFormat(selectedKey);
                  setSelectedTunnelForImport(null);
                  setImportData("");
                  setImportResults([]);
                }
              }}
            >
              <SelectItem key="flyx" textValue="FLYX 格式">
                FLYX 格式（管道分隔）
              </SelectItem>
              <SelectItem key="ny" textValue="ny格式">
                ny格式（JSON）
              </SelectItem>
            </Select>

            {/* 隧道选择 - 两种格式都需要 */}
            <Select
              isRequired
              label="选择导入隧道"
              placeholder="请选择要导入的隧道"
              selectedKeys={
                selectedTunnelForImport
                  ? [selectedTunnelForImport.toString()]
                  : []
              }
              variant="bordered"
              onSelectionChange={(keys) => {
                const selectedKey = Array.from(keys)[0] as string;

                setSelectedTunnelForImport(
                  selectedKey ? parseInt(selectedKey) : null,
                );
              }}
            >
              {tunnels.map((tunnel) => (
                <SelectItem key={tunnel.id.toString()} textValue={tunnel.name}>
                  {tunnel.name}
                </SelectItem>
              ))}
            </Select>

            {/* 输入区域 */}
            <Textarea
              classNames={{
                input: "font-mono text-sm",
              }}
              label="导入数据"
              maxRows={12}
              minRows={8}
              placeholder={
                importFormat === "flyx"
                  ? "请输入要导入的规则数据，格式：目标地址|规则名称|入口端口"
                  : '请输入ny格式数据，每行一个JSON对象，如：{"dest":["1.2.3.4:80"],"listen_port":8080,"name":"规则1"}；listen_port可省略自动分配'
              }
              value={importData}
              variant="flat"
              onChange={(e) => setImportData(e.target.value)}
            />

            {/* 导入结果 */}
            {importResults.length > 0 && (
              <div>
                <div className="flex items-center justify-between mb-2">
                  <h3 className="text-base font-semibold">导入结果</h3>
                  <span className="text-xs text-default-500">
                    成功：{importResults.filter((r) => r.success).length} /
                    总计：{importResults.length}
                  </span>
                </div>

                <div
                  className="max-h-40 overflow-y-auto space-y-1 scrollbar-hide"
                  style={{
                    scrollbarWidth: "thin",
                    scrollbarColor: "rgb(156 163 175) transparent",
                  }}
                >
                  {importResults.map((result, index) => (
                    <div
                      key={index}
                      className={`p-2 rounded border ${
                        result.success
                          ? "bg-success-50 dark:bg-success-100/10 border-success-200 dark:border-success-300/20"
                          : "bg-danger-50 dark:bg-danger-100/10 border-danger-200 dark:border-danger-300/20"
                      }`}
                    >
                      <div className="flex items-center gap-2">
                        {result.success ? (
                          <svg
                            aria-hidden="true"
                            className="w-3 h-3 text-success-600 flex-shrink-0"
                            fill="currentColor"
                            viewBox="0 0 20 20"
                          >
                            <path
                              clipRule="evenodd"
                              d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z"
                              fillRule="evenodd"
                            />
                          </svg>
                        ) : (
                          <svg
                            aria-hidden="true"
                            className="w-3 h-3 text-danger-600 flex-shrink-0"
                            fill="currentColor"
                            viewBox="0 0 20 20"
                          >
                            <path
                              clipRule="evenodd"
                              d="M4.293 4.293a1 1 0 011.414 0L10 8.586l4.293-4.293a1 1 0 111.414 1.414L11.414 10l4.293 4.293a1 1 0 01-1.414 1.414L10 11.414l-4.293 4.293a1 1 0 01-1.414-1.414L8.586 10 4.293 5.707a1 1 0 010-1.414z"
                              fillRule="evenodd"
                            />
                          </svg>
                        )}
                        <div className="flex-1 min-w-0">
                          <div className="flex items-center gap-2 mb-0.5">
                            <span
                              className={`text-xs font-medium ${
                                result.success
                                  ? "text-success-700 dark:text-success-300"
                                  : "text-danger-700 dark:text-danger-300"
                              }`}
                            >
                              {result.success ? "成功" : "失败"}
                            </span>
                            <span className="text-xs text-default-500">|</span>
                            <code className="text-xs font-mono text-default-600 truncate">
                              {result.line}
                            </code>
                          </div>
                          <div
                            className={`text-xs ${
                              result.success
                                ? "text-success-600 dark:text-success-400"
                                : "text-danger-600 dark:text-danger-400"
                            }`}
                          >
                            {result.message}
                          </div>
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>
        </ModalBody>
        <ModalFooter>
          <Button variant="light" onPress={() => setImportModalOpen(false)}>
            取消
          </Button>
          <Button
            color="warning"
            isDisabled={!importData.trim() || !selectedTunnelForImport}
            isLoading={importLoading}
            onPress={executeImport}
          >
            开始导入
          </Button>
        </ModalFooter>
      </ModalContent>
    </Modal>
  );
}
