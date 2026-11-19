import React from "react";
import {
  AbsoluteFill,
  Easing,
  interpolate,
  useCurrentFrame,
  useVideoConfig,
} from "remotion";
import { COLORS, FONT } from "../theme";

export const SceneShell: React.FC<{
  children: React.ReactNode;
  glowX?: string;
  glowY?: string;
}> = ({ children, glowX = "70%", glowY = "20%" }) => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();
  const breathe = interpolate(frame, [0, 6 * fps], [0.55, 0.8], {
    extrapolateRight: "clamp",
    extrapolateLeft: "clamp",
    easing: Easing.bezier(0.4, 0, 0.4, 1),
  });

  return (
    <AbsoluteFill
      style={{
        backgroundColor: COLORS.bg,
        fontFamily: FONT,
        color: COLORS.text,
        overflow: "hidden",
      }}
    >
      <AbsoluteFill
        style={{
          opacity: breathe,
          background: `radial-gradient(900px 520px at ${glowX} ${glowY}, rgba(45,212,191,0.16), transparent 65%),
            radial-gradient(760px 460px at 12% 88%, rgba(76,125,255,0.12), transparent 62%)`,
        }}
      />
      <AbsoluteFill
        style={{
          opacity: 0.16,
          backgroundImage: `linear-gradient(rgba(255,255,255,0.05) 1px, transparent 1px),
            linear-gradient(90deg, rgba(255,255,255,0.05) 1px, transparent 1px)`,
          backgroundSize: "72px 72px",
          maskImage: "radial-gradient(ellipse at 50% 40%, black 30%, transparent 85%)",
        }}
      />
      {children}
    </AbsoluteFill>
  );
};

export const Logo: React.FC<{ size?: number }> = ({ size = 96 }) => {
  return (
    <div
      style={{
        display: "flex",
        alignItems: "center",
        gap: size * 0.28,
      }}
    >
      <div
        style={{
          width: size,
          height: size,
          borderRadius: size * 0.24,
          background: `linear-gradient(145deg, ${COLORS.accent}, ${COLORS.accent2})`,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          color: "#06222A",
          fontSize: size * 0.52,
          fontWeight: 700,
          boxShadow: `0 0 ${size * 0.7}px rgba(45,212,191,0.45)`,
        }}
      >
        遥
      </div>
      <div style={{ display: "flex", flexDirection: "column", gap: size * 0.08 }}>
        <div style={{ fontSize: size * 0.46, fontWeight: 700, letterSpacing: 1 }}>
          Open Voice Bridge
        </div>
        <div style={{ fontSize: size * 0.26, color: COLORS.muted, letterSpacing: 6 }}>
          小遥 · Remote Studio
        </div>
      </div>
    </div>
  );
};

export const SubtitleBar: React.FC<{ text: string; startFrame?: number }> = ({
  text,
  startFrame = 12,
}) => {
  const frame = useCurrentFrame();
  const { fps, durationInFrames } = useVideoConfig();
  const opacity = interpolate(
    frame,
    [startFrame, startFrame + 0.4 * fps, durationInFrames - 0.35 * fps, durationInFrames - 2],
    [0, 1, 1, 0],
    {
      extrapolateLeft: "clamp",
      extrapolateRight: "clamp",
      easing: Easing.bezier(0.16, 1, 0.3, 1),
    },
  );
  return (
    <div
      style={{
        position: "absolute",
        left: 0,
        right: 0,
        bottom: 56,
        display: "flex",
        justifyContent: "center",
        opacity,
      }}
    >
      <div
        style={{
          maxWidth: 1560,
          padding: "18px 42px",
          borderRadius: 18,
          background: "rgba(10,14,23,0.72)",
          border: "1px solid rgba(255,255,255,0.09)",
          backdropFilter: "blur(18px)",
          fontSize: 32,
          fontWeight: 500,
          letterSpacing: 0.5,
          textAlign: "center",
          color: "#E8EEFF",
          lineHeight: 1.5,
        }}
      >
        {text}
      </div>
    </div>
  );
};

export const Chip: React.FC<{
  children: React.ReactNode;
  tone?: "accent" | "blue" | "plain";
  style?: React.CSSProperties;
}> = ({ children, tone = "plain", style }) => {
  const palette =
    tone === "accent"
      ? { bg: "rgba(45,212,191,0.14)", bd: "rgba(45,212,191,0.5)", fg: COLORS.accent }
      : tone === "blue"
        ? { bg: "rgba(76,125,255,0.14)", bd: "rgba(76,125,255,0.5)", fg: "#9DB8FF" }
        : { bg: "rgba(255,255,255,0.05)", bd: "rgba(255,255,255,0.14)", fg: COLORS.text };
  return (
    <div
      style={{
        padding: "12px 26px",
        borderRadius: 999,
        background: palette.bg,
        border: `1px solid ${palette.bd}`,
        color: palette.fg,
        fontSize: 30,
        fontWeight: 600,
        letterSpacing: 1,
        ...style,
      }}
    >
      {children}
    </div>
  );
};
