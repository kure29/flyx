declare module "recharts" {
  import * as React from "react";

  export const LineChart: React.ComponentType<Record<string, unknown>>;
  export const Line: React.ComponentType<Record<string, unknown>>;
  export const ComposedChart: React.ComponentType<Record<string, unknown>>;
  export const Area: React.ComponentType<Record<string, unknown>>;
  export const XAxis: React.ComponentType<Record<string, unknown>>;
  export const YAxis: React.ComponentType<Record<string, unknown>>;
  export const CartesianGrid: React.ComponentType<Record<string, unknown>>;
  export const ReferenceLine: React.ComponentType<Record<string, unknown>>;
  export const Tooltip: React.ComponentType<Record<string, unknown>>;
  export const ResponsiveContainer: React.ComponentType<
    Record<string, unknown>
  >;
}
