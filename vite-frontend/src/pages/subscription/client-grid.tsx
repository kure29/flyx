import { Copy } from "lucide-react";

import { copyText } from "@/pages/inbound/share";
import { SUB_CLIENTS } from "@/pages/subscription/clients";

/**
 * One tile per client: pressing it opens the app's own import (or copies
 * the link where the app has none), the button on its right copies the
 * link. With routing rules, clients that take a full configuration get it.
 */
export function SubClientGrid({
  link,
  name,
  rules,
  configClients = [],
}: {
  /** The subscription link for a ?format= value ("" for auto). */
  link: (format: string) => string;
  name: string;
  rules: boolean;
  /** Clients rendered from a template: they get a full configuration too. */
  configClients?: string[];
}) {
  return (
    <div className="flex flex-wrap gap-1.5 sm:grid sm:grid-cols-4">
      {SUB_CLIENTS.map((c) => {
        const url = link(c.format);
        const isConfig =
          c.configImportURL !== undefined &&
          (rules || configClients.includes(c.key));
        const importURL = isConfig ? (c.configImportURL ?? null) : c.importURL;
        const open = () => {
          if (importURL) {
            window.location.href = importURL(url, name);
          } else {
            copyText(
              url,
              isConfig
                ? `${c.label}配置链接已复制，在 App 的配置里从 URL 下载`
                : `${c.label}订阅已复制`,
            );
          }
        };

        return (
          <div
            key={c.key}
            className="relative flex h-9 items-center rounded-md border border-divider transition-colors hover:bg-default-50"
          >
            <button
              className="h-full min-w-0 flex-1 truncate px-3 text-left text-xs sm:pl-2.5 sm:pr-7"
              title={
                importURL
                  ? `导入到 ${c.label}`
                  : isConfig
                    ? `复制 ${c.label} 配置链接`
                    : "复制通用订阅"
              }
              type="button"
              onClick={open}
            >
              {c.label}
            </button>
            <button
              aria-label={`复制 ${c.label} 订阅链接`}
              className="absolute right-1 top-1/2 hidden -translate-y-1/2 rounded p-1 text-default-300 hover:bg-default-100 hover:text-foreground sm:block"
              type="button"
              onClick={() => copyText(url, `${c.label} 订阅已复制`)}
            >
              <Copy className="h-3 w-3" />
            </button>
          </div>
        );
      })}
    </div>
  );
}
