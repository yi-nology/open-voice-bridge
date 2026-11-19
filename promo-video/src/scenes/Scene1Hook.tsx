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

export const Scene1Hook: React.FC = () => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();

  const line = (start: number) => ({
    opacity: interpolate(frame, [start * fps, (start + 0.7) * fps], [0, 1], {
      extrapolateLeft: "clamp",
      extrapolateRight: "clamp",
      easing: Easing.bezier(0.16, 1, 0.3, 1),
    }),
    translate: interpolate(
      frame,
      [start * fps, (start + 0.7) * fps],
      ["0px 36px", "0px 0px"],
      {
        extrapolateLeft: "clamp",
        extrapolateRight: "clamp",
        easing: Easing.bezier(0.16, 1, 0.3, 1),
      },
    ),
  });

  return (
    <SceneShell glowX="78%" glowY="30%">
      <AbsoluteCenter>
        <Interactive.Div
          name="Hook question"
          style={{ fontSize: 92, fontWeight: 700, letterSpacing: 2, ...line(0.3) }}
        >
          在 Linux 上，想要语音输入？
        </Interactive.Div>
        <Interactive.Div
          name="Hook answer"
          style={{
            fontSize: 60,
            fontWeight: 500,
            marginTop: 40,
            color: COLORS.accent,
            letterSpacing: 3,
            ...line(1.6),
          }}
        >
          你手边的遥控器，就是麦克风。
        </Interactive.Div>
      </AbsoluteCenter>

      <RippleBadge />
      <SubtitleBar text="在 Linux 上想要语音输入，总缺一个顺手的方案。其实你手边的蓝牙遥控器，就可以是麦克风。" />
    </SceneShell>
  );
};

const AbsoluteCenter: React.FC<{ children: React.ReactNode }> = ({ children }) => (
  <div
    style={{
      position: "absolute",
      inset: 0,
      display: "flex",
      flexDirection: "column",
      alignItems: "center",
      justifyContent: "center",
    }}
  >
    {children}
  </div>
);

const RippleBadge: React.FC = () => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();
  return (
    <div style={{ position: "absolute", right: 140, top: 160, width: 320, height: 320 }}>
      {[0, 1, 2].map((i) => {
        const phase = ((frame - 0.8 * fps - i * 0.55 * fps) % (2.4 * fps)) / (2.4 * fps);
        const visible = frame > 0.8 * fps + i * 0.55 * fps;
        return (
          <div
            key={i}
            style={{
              position: "absolute",
              inset: 0,
              borderRadius: "50%",
              border: `2px solid ${COLORS.accent}`,
              opacity: visible ? interpolate(phase, [0, 1], [0.55, 0]) : 0,
              scale: String(interpolate(phase, [0, 1], [0.35, 1.15])),
            }}
          />
        );
      })}
      <div
        style={{
          position: "absolute",
          left: "50%",
          top: "50%",
          translate: "-50% -50%",
          width: 132,
          height: 132,
          borderRadius: "50%",
          background: `radial-gradient(circle at 35% 30%, rgba(45,212,191,0.95), rgba(45,212,191,0.18))`,
          boxShadow: "0 0 70px rgba(45,212,191,0.5)",
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
        }}
      >
        <MicGlyph />
      </div>
    </div>
  );
};

export const MicGlyph: React.FC<{ size?: number; color?: string }> = ({
  size = 54,
  color = "#06222A",
}) => (
  <svg width={size} height={size} viewBox="0 0 24 24" fill="none">
    <rect x="9" y="2.5" width="6" height="11" rx="3" fill={color} />
    <path
      d="M5.5 11a6.5 6.5 0 0 0 13 0"
      stroke={color}
      strokeWidth="1.8"
      strokeLinecap="round"
    />
    <path d="M12 17.5V21" stroke={color} strokeWidth="1.8" strokeLinecap="round" />
  </svg>
);
