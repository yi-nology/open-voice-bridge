import React from "react";
import {
  Easing,
  Img,
  interpolate,
  Interactive,
  staticFile,
  useCurrentFrame,
  useVideoConfig,
} from "remotion";
import { SceneShell, SubtitleBar } from "../components/shell";
import { COLORS } from "../theme";

const DEVICES = [
  { name: "小米遥控器 2 Pro", sub: "RC003 · ATVV 开箱即用" },
  { name: "小米遥控器 2 / ARN9", sub: "米家语音遥控器" },
  { name: "Google TV 遥控器", sub: "含 Onn 同族 · GATT 键位" },
  { name: "通用 ATVV / Siri 1 代", sub: "Generic 配置可选" },
];

const KEYMAP = [
  { k: "语音键", v: "按住说话 · SayAll 短语" },
  { k: "方向 / OK", v: "命名预设 · 自定义宏" },
  { k: "任意键", v: "捕获模式 · 热插拔" },
];

export const Scene4Devices: React.FC = () => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();

  return (
    <SceneShell glowX="24%" glowY="22%">
      <Interactive.Div
        name="Scene title"
        style={{
          position: "absolute",
          top: 108,
          left: 140,
          fontSize: 60,
          fontWeight: 700,
          letterSpacing: 2,
          opacity: interpolate(frame, [0, 0.5 * fps], [0, 1], {
            extrapolateLeft: "clamp",
            extrapolateRight: "clamp",
            easing: Easing.bezier(0.16, 1, 0.3, 1),
          }),
        }}
      >
        主流遥控器，<span style={{ color: COLORS.accent }}>开箱即用</span>
      </Interactive.Div>

      <div style={{ position: "absolute", top: 230, left: 140, width: 780 }}>
        {DEVICES.map((d, i) => {
          const t0 = 0.4 + i * 0.28;
          return (
            <Interactive.Div
              key={d.name}
              name={`Device ${d.name}`}
              style={{
                marginBottom: 22,
                padding: "26px 32px",
                borderRadius: 20,
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
                  ["0px 26px", "0px 0px"],
                  {
                    extrapolateLeft: "clamp",
                    extrapolateRight: "clamp",
                    easing: Easing.bezier(0.16, 1, 0.3, 1),
                  },
                ),
                display: "flex",
                alignItems: "center",
                gap: 24,
              }}
            >
              <div
                style={{
                  width: 44,
                  height: 44,
                  borderRadius: "50%",
                  background: "rgba(45,212,191,0.16)",
                  border: "1.5px solid rgba(45,212,191,0.7)",
                  display: "flex",
                  alignItems: "center",
                  justifyContent: "center",
                  color: COLORS.accent,
                  fontSize: 26,
                  fontWeight: 700,
                  flexShrink: 0,
                }}
              >
                ✓
              </div>
              <div>
                <div style={{ fontSize: 36, fontWeight: 600, letterSpacing: 1 }}>{d.name}</div>
                <div style={{ fontSize: 25, color: COLORS.muted, marginTop: 6, letterSpacing: 1 }}>
                  {d.sub}
                </div>
              </div>
            </Interactive.Div>
          );
        })}
      </div>

      {/* keymap screenshot in device frame */}
      <Interactive.Div
        name="Keymap screenshot"
        style={{
          position: "absolute",
          right: 130,
          top: 190,
          width: 750,
          borderRadius: 22,
          overflow: "hidden",
          border: "1px solid rgba(255,255,255,0.14)",
          boxShadow: "0 30px 90px rgba(0,0,0,0.55), 0 0 60px rgba(76,125,255,0.22)",
          opacity: interpolate(frame, [0.9 * fps, 1.6 * fps], [0, 1], {
            extrapolateLeft: "clamp",
            extrapolateRight: "clamp",
            easing: Easing.bezier(0.16, 1, 0.3, 1),
          }),
          scale: String(
            interpolate(frame, [0.9 * fps, 1.6 * fps], [0.96, 1], {
              extrapolateLeft: "clamp",
              extrapolateRight: "clamp",
              easing: Easing.bezier(0.16, 1, 0.3, 1),
              output: "perceptual-scale",
            }),
          ),
        }}
      >
        <div
          style={{
            height: 42,
            background: "rgba(255,255,255,0.07)",
            display: "flex",
            alignItems: "center",
            gap: 8,
            padding: "0 18px",
          }}
        >
          {["#FF5F57", "#FEBC2E", "#28C840"].map((c) => (
            <div key={c} style={{ width: 12, height: 12, borderRadius: "50%", background: c }} />
          ))}
          <div style={{ marginLeft: 14, fontSize: 20, color: COLORS.muted }}>
            小遥 · 按键映射
          </div>
        </div>
        <Img src={staticFile("assets/keymap.jpg")} style={{ width: "100%", display: "block" }} />
      </Interactive.Div>

      <div
        style={{
          position: "absolute",
          right: 130,
          top: 786,
          display: "flex",
          flexWrap: "nowrap",
          gap: 16,
        }}
      >
        {KEYMAP.map((k, i) => (
          <div
            key={k.k}
            style={{
              padding: "14px 22px",
              borderRadius: 16,
              background: "rgba(76,125,255,0.12)",
              border: "1px solid rgba(76,125,255,0.4)",
              opacity: interpolate(frame, [(1.6 + i * 0.25) * fps, (2.1 + i * 0.25) * fps], [0, 1], {
                extrapolateLeft: "clamp",
                extrapolateRight: "clamp",
                easing: Easing.bezier(0.16, 1, 0.3, 1),
              }),
            }}
          >
            <div style={{ fontSize: 24, fontWeight: 700, color: "#9DB8FF", whiteSpace: "nowrap" }}>{k.k}</div>
            <div style={{ fontSize: 18, color: COLORS.muted, marginTop: 4, whiteSpace: "nowrap" }}>{k.v}</div>
          </div>
        ))}
      </div>

      <SubtitleBar text="小米、Google TV 等主流语音遥控器开箱即用。按键支持完整重映射，命名预设、自定义宏、热插拔，通通不在话下。" />
    </SceneShell>
  );
};
