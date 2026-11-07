import { useState } from "react";
import { X } from "lucide-react";

const DOCS = "https://kure29.github.io/flyx/guide/install";

/** The demo's floating note: sample data and how to install. */
export function DemoBanner() {
  const [open, setOpen] = useState(true);

  if (!open) {
    return (
      <button
        className="fixed bottom-[84px] right-3 z-[60] rounded-full bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground lg:bottom-4"
        type="button"
        onClick={() => setOpen(true)}
      >
        演示站
      </button>
    );
  }

  return (
    <div className="fixed bottom-[84px] right-3 z-[60] w-[min(20rem,calc(100vw-24px))] rounded-2xl border border-divider bg-content1/95 p-3 text-xs shadow-xl backdrop-blur lg:bottom-4">
      <div className="mb-1 flex items-center justify-between">
        <span className="font-semibold text-sm">FLYX 在线演示</span>
        <button
          aria-label="收起"
          className="rounded p-1 text-default-400 hover:bg-default-100"
          type="button"
          onClick={() => setOpen(false)}
        >
          <X className="h-3.5 w-3.5" />
        </button>
      </div>
      <p className="text-default-500">
        数据都是示例，可以随便点；修改不会保存，刷新后还原。
      </p>
      <div className="mt-2 flex gap-2">
        <a
          className="flex flex-1 items-center justify-center rounded-lg bg-primary px-2 py-1.5 font-medium text-primary-foreground"
          href={DOCS}
          rel="noreferrer"
          target="_blank"
        >
          安装 FLYX
        </a>
      </div>
    </div>
  );
}
