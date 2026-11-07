import * as React from "react";

import { Checkbox as BaseCheckbox } from "@/components/ui/checkbox";
import { cn } from "@/lib/utils";

export interface CheckboxProps
  extends Omit<
    React.ComponentProps<typeof BaseCheckbox>,
    "checked" | "onCheckedChange"
  > {
  classNames?: Record<string, string>;
  color?: string;
  isDisabled?: boolean;
  isIndeterminate?: boolean;
  isSelected?: boolean;
  onValueChange?: (value: boolean) => void;
  size?: string;
}

export function Checkbox({
  children,
  className,
  isDisabled,
  isIndeterminate,
  isSelected,
  onValueChange,
  ...props
}: CheckboxProps & {
  children?: React.ReactNode;
}) {
  const handleCheckedChange: React.ComponentProps<
    typeof BaseCheckbox
  >["onCheckedChange"] = (value: boolean | "indeterminate") => {
    onValueChange?.(value === true);
  };

  const checkedValue: boolean | "indeterminate" = isIndeterminate
    ? "indeterminate"
    : Boolean(isSelected);

  // A label so tapping the text toggles the box too (the box alone is a
  // small target on phones).
  return (
    <label
      className={cn(
        "inline-flex items-center gap-2",
        isDisabled ? "opacity-50" : "cursor-pointer",
        className,
      )}
    >
      <BaseCheckbox
        checked={checkedValue}
        disabled={isDisabled}
        onCheckedChange={handleCheckedChange}
        {...props}
      />
      {children ? <span className="text-sm">{children}</span> : null}
    </label>
  );
}
