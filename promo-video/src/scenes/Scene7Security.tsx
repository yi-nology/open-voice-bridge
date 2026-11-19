import React from "react";
import {
  Easing,
  interpolate,
  Interactive,
  useCurrentFrame,
  useVideoConfig,
} from "remotion";
import { SceneShell, SubtitleBar } from "../components/shell";
import { COLORS } from "../theme";

const CARDS = [
  {
    icon: "⌂",
    t: "只监听 127.0.0.1",
    d: "管理界面只在本机开放，不对外暴露端口",
  },
  {
    icon: "🔑",
    t: "API Key 只存路径",
    d: "凭据以文件路径形式保存，不进配置明文",
  },
  {
    icon: "◈",
    t: "只读 MCP 授权",
    d: "为本地 AI 客户端单独发 256-bit 令牌，撤销即失效",
  },
];

const METRICS = ["Prometheus /metrics", "BLE · ATVV · 麦克风", "转写数与时延直方图", "快速检查 / 节点健康"];

export const Scene7Security: React.FC = () => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();

  return (
    <SceneShell glowX="50%" glowY="18%">
      <Interactive.Div
        name="Scene title"
        style={{
          position: "absolute",
          top: 112,
          width: "100%",
          textAlign: "center",
          fontSize: 62,
          fontWeight: 700,
          letterSpacing: 2,
          opacity: interpolate(frame, [0, 0.5 * fps], [0, 1], {
            extrapolateLeft: "clamp",
            extrapolateRight: "clamp",
            easing: Easing.bezier(0.16, 1, 0.3, 1),
          }),
        }}
      >
        为长期运行而设计的<span style={{ color: COLORS.accent }}>安全感</span>
      </Interactive.Div>

      <div
        style={{
          position: "absolute",
          top: 280,
          left: 140,
          right: 140,
          display: "flex",
          gap: 32,
        }}
      >
        {CARDS.map((c, i) => {
          const t0 = 0.35 + i * 0.32;
          return (
            <Interactive.Div
              key={c.t}
              name={`Card ${c.t}`}
              style={{
                flex: 1,
                padding: "42px 40px",
                borderRadius: 24,
                background: "rgba(255,255,255,0.045)",
                border: "1px solid rgba(255,255,255,0.1)",
                opacity: interpolate(frame, [t0 * fps, (t0 + 0.55) * fps], [0, 1], {
                  extrapolateLeft: "clamp",
                  extrapolateRight: "clamp",
                  easing: Easing.bezier(0.16, 1, 0.3, 1),
                }),
                translate: interpolate(
                  frame,
                  [t0 * fps, (t0 + 0.55) * fps],
                  ["0px 34px", "0px 0px"],
                  {
                    extrapolateLeft: "clamp",
                    extrapolateRight: "clamp",
                    easing: Easing.bezier(0.16, 1, 0.3, 1),
                  },
                ),
              }}
            >
              <div
                style={{
                  width: 78,
                  height: 78,
                  borderRadius: 20,
                  background: "rgba(45,212,191,0.12)",
                  border: "1px solid rgba(45,212,191,0.45)",
                  display: "flex",
                  alignItems: "center",
                  justifyContent: "center",
                  fontSize: 38,
                  marginBottom: 26,
                }}
              >
                {c.icon}
              </div>
              <div style={{ fontSize: 40, fontWeight: 700, letterSpacing: 1 }}>{c.t}</div>
              <div
                style={{
                  fontSize: 27,
                  color: COLORS.muted,
                  marginTop: 16,
                  lineHeight: 1.7,
                  letterSpacing: 1,
                }}
              >
                {c.d}
              </div>
            </Interactive.Div>
          );
        })}
      </div>

      {/* metrics strip */}
      <div
        style={{
          position: "absolute",
          top: 700,
          left: 140,
          right: 140,
          display: "flex",
          gap: 22,
        }}
      >
        {METRICS.map((m, i) => {
          const t0 = 1.9 + i * 0.22;
          return (
            <div
              key={m}
              style={{
                flex: 1,
                textAlign: "center",
                padding: "24px 12px",
                borderRadius: 16,
                background: "rgba(76,125,255,0.1)",
                border: "1px solid rgba(76,125,255,0.35)",
                fontSize: 27,
                color: "#B9CBFF",
                letterSpacing: 1,
                opacity: interpolate(frame, [t0 * fps, (t0 + 0.45) * fps], [0, 1], {
                  extrapolateLeft: "clamp",
                  extrapolateRight: "clamp",
                  easing: Easing.bezier(0.16, 1, 0.3, 1),
                }),
              }}
            >
              {m}
            </div>
          );
        })}
      </div>

      <SubtitleBar text="管理界面只监听本机，API Key 只以文件路径保存。还能为本地 AI 客户端创建只读授权，撤销即失效。状态、耗时、指标，一目了然。" />
    </SceneShell>
  );
};
