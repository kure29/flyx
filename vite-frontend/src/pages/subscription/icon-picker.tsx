import type { SubIconLibrary, SubIconLibraryIcon } from "@/api";

import { useEffect, useState } from "react";
import toast from "react-hot-toast";
import { ChevronLeft, Search, Trash2 } from "lucide-react";

import {
  deleteSubIconLibrary,
  getSubIconLibraries,
  getSubIconLibraryIcons,
  saveSubIconLibrary,
} from "@/api";
import { Button } from "@/shadcn-bridge/heroui/button";
import {
  Modal,
  ModalBody,
  ModalContent,
  ModalHeader,
} from "@/shadcn-bridge/heroui/modal";

const selectClass =
  "h-9 min-w-0 rounded-lg border border-divider bg-background px-2 text-sm";

/** A group's icon; a blank square when there is none or it fails to load. */
export function GroupIcon({
  src,
  size = "h-5 w-5",
}: {
  src?: string;
  size?: string;
}) {
  const [failed, setFailed] = useState(false);

  useEffect(() => setFailed(false), [src]);

  if (!src || failed)
    return (
      <span aria-hidden className={`${size} shrink-0 rounded bg-default-100`} />
    );

  return (
    <img
      alt=""
      className={`${size} shrink-0 object-contain`}
      loading="lazy"
      src={src}
      onError={() => setFailed(true)}
    />
  );
}

// Libraries are downloaded by the panel once per page load.
const iconCache = new Map<string, Promise<SubIconLibraryIcon[]>>();

export function loadLibraryIcons(url: string): Promise<SubIconLibraryIcon[]> {
  let p = iconCache.get(url);

  if (!p) {
    p = getSubIconLibraryIcons(url).then((res) => {
      if (res.code !== 0 || !res.data) {
        iconCache.delete(url);
        throw new Error(res.msg || "图标库加载失败");
      }

      return res.data.icons;
    });
    p.catch(() => iconCache.delete(url));
    iconCache.set(url, p);
  }

  return p;
}

const normalize = (s: string) =>
  s
    .replace(/\.(png|jpe?g|svg|webp|gif)$/i, "")
    .toLowerCase()
    .replace(/[^a-z0-9一-鿿]/g, "");

/** The first icon whose file name matches one of the names, in order. */
export function matchIcon(
  icons: SubIconLibraryIcon[],
  names: string[],
): string | undefined {
  const byName = new Map<string, string>();

  for (const icon of icons) {
    const key = normalize(icon.name);

    if (!byName.has(key)) byName.set(key, icon.url);
  }
  for (const name of names) {
    const url = byName.get(normalize(name));

    if (url) return url;
  }

  return undefined;
}

export function useIconLibraries() {
  const [libraries, setLibraries] = useState<SubIconLibrary[]>([]);
  const [defaults, setDefaults] = useState<Record<string, string>>({});

  useEffect(() => {
    let active = true;

    getSubIconLibraries()
      .then((res) => {
        if (active && res.code === 0 && res.data) {
          setLibraries(res.data.libraries);
          setDefaults(res.data.defaults);
        }
      })
      .catch(() => {});

    return () => {
      active = false;
    };
  }, []);

  return { libraries, setLibraries, defaults };
}

/**
 * Picks a group icon from an icon library or a pasted image address, and
 * manages the libraries.
 */
export function IconPicker({
  isOpen,
  title,
  value,
  fallback,
  libraries,
  library,
  onLibrary,
  onLibraries,
  onPick,
  onClose,
}: {
  isOpen: boolean;
  title: string;
  /** The chosen icon; empty uses the default. */
  value: string;
  fallback?: string;
  libraries: SubIconLibrary[];
  library: string;
  onLibrary: (url: string) => void;
  onLibraries: (libraries: SubIconLibrary[]) => void;
  /** An image URL, or "" for the default. */
  onPick: (url: string) => void;
  onClose: () => void;
}) {
  const [icons, setIcons] = useState<SubIconLibraryIcon[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [query, setQuery] = useState("");
  const [custom, setCustom] = useState("");
  const [newLibrary, setNewLibrary] = useState("");
  const [adding, setAdding] = useState(false);

  useEffect(() => {
    if (!isOpen || !library) return;
    let active = true;

    setLoading(true);
    setError("");
    loadLibraryIcons(library)
      .then((list) => active && setIcons(list))
      .catch((e: Error) => {
        if (active) {
          setIcons([]);
          setError(e.message);
        }
      })
      .finally(() => active && setLoading(false));

    return () => {
      active = false;
    };
  }, [isOpen, library]);

  useEffect(() => {
    if (isOpen) {
      setQuery("");
      setCustom(value);
    }
  }, [isOpen, value]);

  const current = libraries.find((l) => l.url === library);
  const q = normalize(query);
  const shown = q ? icons.filter((i) => normalize(i.name).includes(q)) : icons;
  const customValid = /^https?:\/\/[^\s,"'`\\]+$/.test(custom.trim());

  const pick = (url: string) => {
    onPick(url);
    onClose();
  };

  const addLibrary = async () => {
    setAdding(true);
    try {
      const res = await saveSubIconLibrary(newLibrary.trim());

      if (res.code !== 0 || !res.data) {
        toast.error(res.msg || "添加失败");

        return;
      }
      onLibraries(res.data);
      onLibrary(res.data[res.data.length - 1].url);
      setNewLibrary("");
      toast.success("已添加图标库");
    } finally {
      setAdding(false);
    }
  };

  const removeLibrary = async () => {
    if (!current || current.builtin) return;
    const res = await deleteSubIconLibrary(current.url);

    if (res.code !== 0 || !res.data) {
      toast.error(res.msg || "删除失败");

      return;
    }
    onLibraries(res.data);
    onLibrary(res.data[0]?.url ?? "");
  };

  return (
    <Modal
      classNames={{
        base: "!w-[calc(100%-32px)] !mx-auto sm:!w-full overflow-hidden",
      }}
      isOpen={isOpen}
      placement="center"
      scrollBehavior="inside"
      size="lg"
      onOpenChange={(open) => !open && onClose()}
    >
      <ModalContent>
        <ModalHeader className="flex items-center gap-2">
          <button
            aria-label="返回"
            className="rounded-lg p-1 text-default-500 hover:bg-default-100"
            type="button"
            onClick={onClose}
          >
            <ChevronLeft className="h-5 w-5" />
          </button>
          <h2 className="min-w-0 truncate text-lg font-bold">{title} · 图标</h2>
        </ModalHeader>
        <div className="flex shrink-0 flex-col gap-2">
          <div className="flex items-center gap-2">
            <select
              aria-label="图标库"
              className={`${selectClass} flex-1`}
              value={library}
              onChange={(e) => onLibrary(e.target.value)}
            >
              {libraries.map((l) => (
                <option key={l.url} value={l.url}>
                  {l.name}
                  {l.builtin ? "（内置）" : ""}
                </option>
              ))}
            </select>
            {current && !current.builtin && (
              <button
                aria-label="删除这个图标库"
                className="p-1 text-danger"
                type="button"
                onClick={removeLibrary}
              >
                <Trash2 className="h-4 w-4" />
              </button>
            )}
          </div>
          <label className="flex items-center gap-2 rounded-xl border border-divider bg-content1 px-3 py-2">
            <Search className="h-4 w-4 text-default-400" />
            <input
              className="min-w-0 flex-1 bg-transparent text-sm outline-none"
              placeholder="搜索图标名称，例如 YouTube"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
            />
          </label>
        </div>
        <ModalBody className="space-y-4 pb-4">
          {loading ? (
            <p className="py-6 text-center text-sm text-default-400">
              正在加载图标库…
            </p>
          ) : error ? (
            <p className="py-6 text-center text-sm text-danger">{error}</p>
          ) : (
            <div className="grid grid-cols-5 gap-2 sm:grid-cols-7">
              {shown.slice(0, 300).map((icon) => (
                <button
                  key={icon.url}
                  className={`flex flex-col items-center gap-1 rounded-lg p-1.5 text-center hover:bg-default-100 ${value === icon.url ? "bg-primary/10 ring-1 ring-primary" : ""}`}
                  title={icon.name}
                  type="button"
                  onClick={() => pick(icon.url)}
                >
                  <GroupIcon size="h-9 w-9" src={icon.url} />
                  <span className="w-full truncate text-[10px] text-default-500">
                    {icon.name.replace(/\.(png|jpe?g|svg|webp|gif)$/i, "")}
                  </span>
                </button>
              ))}
              {!shown.length && (
                <p className="col-span-full py-6 text-center text-sm text-default-400">
                  没有匹配的图标
                </p>
              )}
              {shown.length > 300 && (
                <p className="col-span-full text-center text-xs text-default-400">
                  只显示前 300 个，请输入名称搜索
                </p>
              )}
            </div>
          )}

          <div className="space-y-2 rounded-xl border border-divider p-3">
            <p className="text-sm font-medium">使用图片地址</p>
            <div className="flex items-center gap-2">
              <GroupIcon src={customValid ? custom.trim() : undefined} />
              <input
                aria-label="图标图片地址"
                className={`${selectClass} flex-1`}
                placeholder="https://…/icon.png"
                value={custom}
                onChange={(e) => setCustom(e.target.value)}
              />
              <Button
                isDisabled={!customValid}
                size="sm"
                variant="flat"
                onPress={() => pick(custom.trim())}
              >
                使用
              </Button>
            </div>
            <div className="flex items-center justify-between gap-2">
              <span className="flex items-center gap-2 text-xs text-default-500">
                默认 <GroupIcon src={fallback} />
                {!fallback && "无图标"}
              </span>
              <Button
                isDisabled={!value}
                size="sm"
                variant="light"
                onPress={() => pick("")}
              >
                恢复默认
              </Button>
            </div>
          </div>

          <div className="space-y-2 rounded-xl border border-divider p-3">
            <p className="text-sm font-medium">添加图标库</p>
            <p className="text-xs text-default-500">
              填写图标订阅 JSON 地址（Quantumult X / Loon / BoxJs 通用格式，含
              name 和 icons），由面板下载。
            </p>
            <div className="flex items-center gap-2">
              <input
                aria-label="图标库地址"
                className={`${selectClass} flex-1`}
                placeholder="https://…/icons.json"
                value={newLibrary}
                onChange={(e) => setNewLibrary(e.target.value)}
              />
              <Button
                isDisabled={!/^https?:\/\/\S+$/.test(newLibrary.trim())}
                isLoading={adding}
                size="sm"
                variant="flat"
                onPress={addLibrary}
              >
                添加
              </Button>
            </div>
          </div>
        </ModalBody>
      </ModalContent>
    </Modal>
  );
}
