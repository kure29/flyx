import { Button } from "@/shadcn-bridge/heroui/button";
import { Input } from "@/shadcn-bridge/heroui/input";
import { SearchIcon } from "@/components/icons";

interface SearchBarProps {
  isVisible: boolean;
  value: string;
  placeholder?: string;
  onOpen: () => void;
  onClose: () => void;
  onChange: (value: string) => void;
}

export function SearchBar({
  isVisible,
  value,
  placeholder = "搜索",
  onOpen,
  onClose,
  onChange,
}: SearchBarProps) {
  return (
    // Fixed h-8 so the container never changes height — eliminates the vertical jitter
    <div className="flex items-center gap-2 h-8 overflow-hidden">
      {!isVisible ? (
        <div key="search-btn">
          <Button
            isIconOnly
            aria-label="搜索"
            className="text-default-600"
            color="default"
            size="sm"
            variant="flat"
            onPress={onOpen}
          >
            <SearchIcon className="w-4 h-4" />
          </Button>
        </div>
      ) : (
        <div key="search-input" className="flex w-full items-center gap-2">
          <Input
            classNames={{
              base: "bg-default-100",
              input:
                "bg-transparent text-sm focus-visible:ring-0 focus-visible:ring-offset-0 focus-visible:outline-none",
              inputWrapper: "bg-default-100 border-0 shadow-none h-8 min-h-8",
            }}
            placeholder={placeholder}
            value={value}
            onChange={(e) => onChange(e.target.value)}
          />
          <Button
            isIconOnly
            aria-label="关闭搜索"
            className="text-default-600 shrink-0"
            color="default"
            size="sm"
            variant="light"
            onPress={() => {
              onClose();
              onChange("");
            }}
          >
            <svg
              aria-hidden="true"
              className="w-4 h-4"
              fill="none"
              stroke="currentColor"
              viewBox="0 0 24 24"
            >
              <path
                d="M6 18L18 6M6 6l12 12"
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={2}
              />
            </svg>
          </Button>
        </div>
      )}
    </div>
  );
}
