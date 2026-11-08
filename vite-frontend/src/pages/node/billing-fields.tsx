import { Input } from "@/shadcn-bridge/heroui/input";
import { Select, SelectItem } from "@/shadcn-bridge/heroui/select";
import {
  cycleFrom,
  cycleParts,
  getNodeRenewalSnapshot,
} from "@/pages/node/renewal";

/** A server's price and renewal as edited. */
export interface BillingValue {
  /** As typed; "" or 0 is free. */
  price: string;
  /** ISO 4217 code; "" is CNY. */
  currency: string;
  /** "" (none), "month", "quarter", "year", "<n>m", "<n>y". */
  renewalCycle: string;
  /** Next expiry (ms), 0 = none. */
  expiryTime: number;
}

export const CURRENCIES: Array<[string, string]> = [
  ["CNY", "人民币"],
  ["USD", "美元"],
  ["HKD", "港币"],
  ["EUR", "欧元"],
  ["JPY", "日元"],
  ["GBP", "英镑"],
  ["TWD", "新台币"],
  ["SGD", "新加坡元"],
  ["KRW", "韩元"],
];

const UNITS: Array<["m" | "y", string]> = [
  ["m", "月"],
  ["y", "年"],
];

const dateValue = (ms: number) => {
  if (!(ms > 0)) return "";
  const d = new Date(ms);
  const pad = (n: number) => String(n).padStart(2, "0");

  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
};

/** Price, currency, payment cycle and expiry: two rows of two. */
export function BillingFields({
  value,
  errors,
  onChange,
}: {
  value: BillingValue;
  errors?: Record<string, string>;
  onChange: (next: Partial<BillingValue>) => void;
}) {
  const parts = cycleParts(value.renewalCycle);
  const count = parts?.count ?? 0;
  const unit = parts?.unit ?? "y";
  const currency = value.currency || "CNY";
  const currencies = CURRENCIES.some(([c]) => c === currency)
    ? CURRENCIES
    : [...CURRENCIES, [currency, currency] as [string, string]];
  const snapshot = getNodeRenewalSnapshot(value.expiryTime, value.renewalCycle);
  const setCycle = (n: number, u: "m" | "y") =>
    onChange({ renewalCycle: n > 0 ? cycleFrom(n, u) : "" });

  return (
    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
      <Input
        description="留空或 0 为免费"
        errorMessage={errors?.price}
        inputMode="decimal"
        isInvalid={!!errors?.price}
        label="价格"
        placeholder="0"
        value={value.price}
        variant="bordered"
        onChange={(e) =>
          onChange({ price: e.target.value.replace(/[^\d.]/g, "") })
        }
      />
      <Select
        description={currencies.find(([c]) => c === currency)?.[1]}
        label="货币"
        selectedKeys={[currency]}
        variant="bordered"
        onSelectionChange={(keys) => {
          const k = Array.from(keys)[0] as string | undefined;

          if (k) onChange({ currency: k === "CNY" ? "" : k });
        }}
      >
        {currencies.map(([code, name]) => (
          <SelectItem key={code} textValue={`${code} ${name}`}>
            {code} {name}
          </SelectItem>
        ))}
      </Select>
      <div className="space-y-1.5">
        <p className="text-sm">付款周期</p>
        <div className="grid grid-cols-[5rem_minmax(0,1fr)] gap-2">
          <Input
            aria-label="付款周期数"
            inputMode="numeric"
            placeholder="—"
            value={count ? String(count) : ""}
            variant="bordered"
            onChange={(e) => {
              const n = Number(e.target.value.replace(/[^\d]/g, "")) || 0;

              setCycle(Math.min(n, unit === "y" ? 10 : 120), unit);
            }}
          />
          <Select
            aria-label="付款周期单位"
            selectedKeys={[unit]}
            variant="bordered"
            onSelectionChange={(keys) => {
              const u = (Array.from(keys)[0] as "m" | "y") || "y";

              setCycle(count || 1, u);
            }}
          >
            {UNITS.map(([k, label]) => (
              <SelectItem key={k} textValue={label}>
                {label}
              </SelectItem>
            ))}
          </Select>
        </div>
        <p className="text-xs text-default-400">
          {count ? "到期后按周期自动顺延" : "留空为不续费"}
        </p>
      </div>
      <Input
        description={
          errors?.expiryTime ||
          (snapshot.nextDueTime && snapshot.nextDueTime !== value.expiryTime
            ? `下次续费 ${dateValue(snapshot.nextDueTime)}`
            : snapshot.diffDays !== undefined
              ? snapshot.diffDays > 0
                ? `${snapshot.diffDays} 天后到期`
                : "今天到期"
              : undefined)
        }
        errorMessage={errors?.expiryTime}
        isInvalid={!!errors?.expiryTime}
        label="到期时间"
        max="9999-12-31"
        type="date"
        value={dateValue(value.expiryTime)}
        variant="bordered"
        onChange={(e) => {
          const [y, m, d] = e.target.value.split("-").map(Number);

          onChange({
            expiryTime: e.target.value ? new Date(y, m - 1, d).getTime() : 0,
          });
        }}
      />
    </div>
  );
}
