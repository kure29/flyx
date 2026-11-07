import { formatTraffic } from "@/utils/traffic";
// 格式化速度
export const formatSpeed = (bytesPerSecond: number): string => {
  if (bytesPerSecond === 0) return "0 B/s";

  const k = 1024;
  const sizes = ["B/s", "KB/s", "MB/s", "GB/s", "TB/s"];
  const i = Math.floor(Math.log(bytesPerSecond) / Math.log(k));

  return (
    parseFloat((bytesPerSecond / Math.pow(k, i)).toFixed(2)) + " " + sizes[i]
  );
};

// 格式化开机时间
export const formatUptime = (seconds: number): string => {
  if (seconds === 0) return "-";

  const days = Math.floor(seconds / 86400);
  const hours = Math.floor((seconds % 86400) / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);

  if (days > 0) {
    return `${days}天${hours}小时`;
  } else if (hours > 0) {
    return `${hours}小时${minutes}分钟`;
  } else {
    return `${minutes}分钟`;
  }
};

// 格式化流量

export const formatFlow = (bytes: number): string => {
  return formatTraffic(bytes);
};

export const formatChainType = (chainType: number, hopInx: number) => {
  if (chainType === 1) {
    return "入口服务器";
  }
  if (chainType === 2) {
    return `中继跳点 #${hopInx}`;
  }
  if (chainType === 3) {
    return "出口服务器";
  }

  return "未知链路";
};
