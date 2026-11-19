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

export const Scene2Logo: React.FC = () => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();

  const logoScale = interpolate(frame, [0.2 * fps, 1.1 * fps], [0.82, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: Easing.bezier(0.16, 1, 0.3, 1),
    output: "perceptual-scale",
  });
  const logoOpacity = interpolate(frame, [0.2 * fps, 0.9 * fps], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: Easing.bezier(0.16, 1, 0.3, 1),
  });
  const tagOpacity = interpolate(frame, [1.4 * fps, 2.2 * fps], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: Easing.bezier(0.16, 1, 0.3, 1),
  });
  const tagTranslate = interpolate(frame, [1.4 * fps, 2.2 * fps], ["0px 28px", "0px 0px"], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: Easing.bezier(0.16, 1, 0.3, 1),
  });

  return (
    <SceneShell glowX="50%" glowY="36%">
      <div
        style={{
          position: "absolute",
          inset: 0,
          display: "flex",
          flexDirection: "column",
          alignItems: "center",
          justifyContent: "center",
          gap: 56,
        }}
      >
        <Interactive.Div name="Logo lockup" style={{ opacity: logoOpacity, scale: String(logoScale) }}>
          <Logo size={110} />
        </Interactive.Div>
        <Interactive.Div
          name="Positioning line"
          style={{
            opacity: tagOpacity,
            translate: tagTranslate,
            fontSize: 52,
            fontWeight: 500,
            letterSpacing: 3,
            textAlign: "center",
            lineHeight: 1.6,
            maxWidth: 1500,
          }}
        >
          把蓝牙遥控器和手机，
          <span style={{ color: COLORS.accent }}>变成 Linux 的本地语音输入服务</span>
        </Interactive.Div>
        <Interactive.Div
          name="Value chips"
          style={{
            opacity: tagOpacity,
            display: "flex",
            gap: 22,
            marginTop: 8,
          }}
        >
          {["本地运行", "不依赖厂商 App", "长期可用"].map((t) => (
            <div
              key={t}
              style={{
                padding: "12px 28px",
                borderRadius: 999,
                border: "1px solid rgba(255,255,255,0.16)",
                background: "rgba(255,255,255,0.04)",
                fontSize: 30,
                color: "#C9D4E8",
                letterSpacing: 2,
              }}
            >
              {t}
            </div>
          ))}
        </Interactive.Div>
      </div>
      <SubtitleBar text="Open Voice Bridge，小遥——把蓝牙遥控器和手机，变成一套可长期运行的本地语音输入服务。" />
    </SceneShell>
  );
};
