/**
 * Cyberpunk Button — Component Override Example
 * ==============================================
 * Demonstrates how to override a built-in component.
 *
 * Rules:
 * 1. Accept the SAME props as the original component.
 * 2. Import the original's props type for compatibility.
 * 3. You CAN wrap the original component and add extra behaviour,
 *    or you can build a completely new component from scratch.
 */

import type { ButtonProps } from "@/shadcn-bridge/heroui/button";

import React from "react";

// Import the original Button's props interface for compatibility
// Optionally import the original to wrap it
import { Button as OriginalButton } from "@/shadcn-bridge/heroui/button";

/**
 * CyberpunkButton wraps the original Button and adds a neon glow effect.
 * It passes all props through, so it's a full drop-in replacement.
 */
export const CyberpunkButton: React.FC<ButtonProps> = (props) => {
  const { className = "", style, color, ...rest } = props;

  // Add neon glow based on color
  const glowColor =
    color === "danger"
      ? "rgba(255, 51, 102, 0.5)"
      : color === "success"
        ? "rgba(0, 255, 136, 0.5)"
        : color === "warning"
          ? "rgba(255, 170, 0, 0.5)"
          : color === "secondary"
            ? "rgba(0, 255, 255, 0.5)"
            : "rgba(255, 0, 255, 0.5)";

  const glowStyle: React.CSSProperties = {
    ...style,
    boxShadow: `0 0 8px ${glowColor}, 0 0 16px ${glowColor}`,
    transition: "box-shadow 0.3s ease, transform 0.15s ease",
    textTransform: "uppercase" as const,
    letterSpacing: "0.05em",
  };

  return (
    <OriginalButton
      className={`${className} cyberpunk-btn`}
      color={color}
      style={glowStyle}
      {...rest}
    />
  );
};
