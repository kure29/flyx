import * as React from "react";

import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";

type ChipColor =
  | "default"
  | "primary"
  | "secondary"
  | "success"
  | "warning"
  | "danger";
type ChipVariant = "solid" | "flat" | "light" | "bordered";
type ChipSize = "sm" | "md" | "lg";

export interface ChipProps extends React.ComponentProps<"span"> {
  color?: ChipColor;
  size?: ChipSize;
  variant?: ChipVariant;
}

function colorVariant(color: ChipColor) {
  if (color === "danger") {
    return "destructive" as const;
  }
  if (color === "success") {
    return "success" as const;
  }
  if (color === "warning") {
    return "warning" as const;
  }
  if (color === "secondary") {
    return "secondary" as const;
  }

  return "default" as const;
}

const SOFT: Record<ChipColor, string> = {
  default: "border-transparent bg-default-100 text-default-600",
  primary: "border-transparent bg-default-100 text-default-600",
  secondary: "border-transparent bg-default-100 text-default-600",
  success:
    "border-transparent bg-success/10 text-success-700 dark:text-success-400",
  warning:
    "border-transparent bg-warning/10 text-warning-700 dark:text-warning-400",
  danger: "border-transparent bg-danger/10 text-danger dark:text-danger-400",
};

export function Chip({
  children,
  className,
  color = "default",
  size = "md",
  variant = "solid",
  ...props
}: ChipProps) {
  return (
    <Badge
      className={cn(
        // Tailwind v4 dropped bg-opacity-*: soft chips set their own tint.
        variant === "flat" || variant === "light" ? SOFT[color] : "",
        variant === "bordered" ? "border-current bg-transparent" : "",
        size === "sm" ? "px-1.5 py-0 text-[11px]" : "",
        size === "lg" ? "px-3 py-1 text-sm" : "",
        className,
      )}
      variant={colorVariant(color)}
      {...props}
    >
      {children}
    </Badge>
  );
}
