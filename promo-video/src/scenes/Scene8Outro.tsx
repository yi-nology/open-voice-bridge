import React from "react";
import {
  Easing,
  interpolate,
  Interactive,
  useCurrentFrame,
  useVideoConfig,
} from "remotion";
import { Logo, SceneShell, SubtitleBar } from "../components/shell";
import { COLORS } from "../theme";

export const Scene8Outro: React.FC = () => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();

  const slogan = "现在，让你的遥控器开口说话。";

  return (
    <SceneShell glowX="50%" glowY="40%">
      <div
        style={{
          position: "absolute",
          inset: 0,
          display: "flex",
          flexDirection: "column",
          alignItems: "center",
          justifyContent: "center",
          gap: 44,
        }}
      >
        <Interactive.Div
          name="Run-anywhere line"
          style={{
            display: "flex",
            gap: 22,
            opacity: interpolate(frame, [0.2 * fps, 0.8 * fps], [0, 1], {
              extrapolateLeft: "clamp",
              extrapolateRight: "clamp",
              easing: Easing.bezier(0.16, 1, 0.3, 1),
            }),
          }}
        >
          {["Raspberry Pi 500", "Debian · PipeWire · BlueZ", "GPL-3.0 开源"].map((t) => (
            <div
              key={t}
              style={{
                padding: "14px 30px",
                borderRadius: 999,
                border: "1px solid rgba(255,255,255,0.16)",
                background: "rgba(255,255,255,0.045)",
                fontSize: 30,
                color: "#C9D4E8",
                letterSpacing: 2,
              }}
            >
              {t}
            </div>
          ))}
        </Interactive.Div>

        <Interactive.Div
          name="Slogan"
          style={{
            fontSize: 88,
            fontWeight: 700,
            letterSpacing: 4,
            textAlign: "center",
            lineHeight: 1.45,
            maxWidth: 1600,
            opacity: interpolate(frame, [1.1 * fps, 1.9 * fps], [0, 1], {
              extrapolateLeft: "clamp",
              extrapolateRight: "clamp",
              easing: Easing.bezier(0.16, 1, 0.3, 1),
            }),
            translate: interpolate(
              frame,
              [1.1 * fps, 1.9 * fps],
              ["0px 34px", "0px 0px"],
              {
                extrapolateLeft: "clamp",
                extrapolateRight: "clamp",
                easing: Easing.bezier(0.16, 1, 0.3, 1),
              },
            ),
          }}
        >
          {slogan.split("，").map((part, i) => (
            <React.Fragment key={part}>
              {i > 0 ? "，" : null}
              <span style={{ color: i === 1 ? COLORS.accent : COLORS.text }}>{part}</span>
            </React.Fragment>
          ))}
        </Interactive.Div>

        <Interactive.Div
          name="Logo outro"
          style={{
            marginTop: 18,
            opacity: interpolate(frame, [3.4 * fps, 4.3 * fps], [0, 1], {
              extrapolateLeft: "clamp",
              extrapolateRight: "clamp",
              easing: Easing.bezier(0.16, 1, 0.3, 1),
            }),
            scale: String(
              interpolate(frame, [3.4 * fps, 4.3 * fps], [0.94, 1], {
                extrapolateLeft: "clamp",
                extrapolateRight: "clamp",
                easing: Easing.bezier(0.16, 1, 0.3, 1),
                output: "perceptual-scale",
              }),
            ),
          }}
        >
          <Logo size={92} />
        </Interactive.Div>

        <Interactive.Div
          name="Repo line"
          style={{
            fontSize: 34,
            color: COLORS.muted,
            letterSpacing: 3,
            opacity: interpolate(frame, [4.3 * fps, 5.1 * fps], [0, 1], {
              extrapolateLeft: "clamp",
              extrapolateRight: "clamp",
              easing: Easing.bezier(0.16, 1, 0.3, 1),
            }),
          }}
        >
          github.com / yi-nology / open-voice-bridge
        </Interactive.Div>
      </div>

      <SubtitleBar text="在 Raspberry Pi 或任何 Linux 主机上即可运行。开源 GPL 三点零，现在就让你的遥控器，开口说话。" startFrame={15} />
    </SceneShell>
  );
};
