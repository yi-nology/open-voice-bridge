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

const LYRICS = ["把光标放进输入框，", "按住语音键直接说话。", "识别结果，就出现在这里。"];

export const Scene6Subtitle: React.FC = () => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();

  return (
    <SceneShell glowX="36%" glowY="26%">
      <Interactive.Div
        name="Scene title"
        style={{
          position: "absolute",
          top: 100,
          left: 140,
          fontSize: 58,
          fontWeight: 700,
          letterSpacing: 2,
          opacity: interpolate(frame, [0, 0.5 * fps], [0, 1], {
            extrapolateLeft: "clamp",
            extrapolateRight: "clamp",
            easing: Easing.bezier(0.16, 1, 0.3, 1),
          }),
        }}
      >
        歌词式字幕 · <span style={{ color: COLORS.accent }}>本地历史</span> · 手机随身
      </Interactive.Div>

      {/* lyric glass screen */}
      <Interactive.Div
        name="Lyric screen"
        style={{
          position: "absolute",
          left: 140,
          top: 230,
          width: 1020,
          height: 500,
          borderRadius: 28,
          background: "rgba(18,26,43,0.82)",
          border: "1px solid rgba(255,255,255,0.12)",
          boxShadow: "0 30px 90px rgba(0,0,0,0.5), 0 0 70px rgba(45,212,191,0.16)",
          backdropFilter: "blur(24px)",
          padding: "56px 64px",
          display: "flex",
          flexDirection: "column",
          gap: 22,
          justifyContent: "center",
        }}
      >
        {LYRICS.map((line, li) => {
          const lineStart = 1.1 * fps + li * 2.6 * fps;
          return (
            <div key={line} style={{ display: "flex", flexWrap: "wrap" }}>
              {line.split("").map((ch, ci) => {
                const charAt = lineStart + ci * 2.4;
                const on = frame >= charAt;
                return (
                  <span
                    key={ci}
                    style={{
                      fontSize: 52,
                      fontWeight: 600,
                      letterSpacing: 2,
                      lineHeight: 1.55,
                      color: on ? COLORS.accent : "rgba(242,246,255,0.2)",
                    }}
                  >
                    {ch}
                  </span>
                );
              })}
            </div>
          );
        })}
        <div
          style={{
            marginTop: 18,
            display: "flex",
            alignItems: "center",
            gap: 16,
            opacity: interpolate(frame, [1.2 * fps, 1.8 * fps], [0, 1], {
              extrapolateLeft: "clamp",
              extrapolateRight: "clamp",
              easing: Easing.bezier(0.16, 1, 0.3, 1),
            }),
          }}
        >
          <div
            style={{
              width: 14,
              height: 14,
              borderRadius: "50%",
              background: COLORS.accent,
              boxShadow: "0 0 16px rgba(45,212,191,0.9)",
            }}
          />
          <div style={{ fontSize: 26, color: COLORS.muted, letterSpacing: 2 }}>
            聆听中 · 聚焦文字，逐句高亮
          </div>
        </div>
      </Interactive.Div>

      {/* phone mock */}
      <Interactive.Div
        name="Phone mock"
        style={{
          position: "absolute",
          right: 180,
          top: 200,
          width: 320,
          borderRadius: 42,
          overflow: "hidden",
          border: "10px solid #1A2334",
          boxShadow: "0 30px 80px rgba(0,0,0,0.55), 0 0 50px rgba(76,125,255,0.25)",
          opacity: interpolate(frame, [0.8 * fps, 1.5 * fps], [0, 1], {
            extrapolateLeft: "clamp",
            extrapolateRight: "clamp",
            easing: Easing.bezier(0.16, 1, 0.3, 1),
          }),
          translate: interpolate(
            frame,
            [0.8 * fps, 1.5 * fps],
            ["0px 40px", "0px 0px"],
            {
              extrapolateLeft: "clamp",
              extrapolateRight: "clamp",
              easing: Easing.bezier(0.16, 1, 0.3, 1),
            },
          ),
        }}
      >
        <Img src={staticFile("assets/overview-mobile.jpg")} style={{ width: "100%", display: "block" }} />
      </Interactive.Div>

      {/* feature chips */}
      <div style={{ position: "absolute", left: 140, top: 780, display: "flex", gap: 20 }}>
        {["本地 JSONL · 最近 200 条", "搜索 / 复制 / 导出 · 不上传", "手机 = 外置麦克风 + 虚拟遥控器"].map(
          (t, i) => (
            <div
              key={t}
              style={{
                padding: "16px 28px",
                borderRadius: 999,
                background: "rgba(255,255,255,0.05)",
                border: "1px solid rgba(255,255,255,0.13)",
                fontSize: 26,
                letterSpacing: 1,
                color: "#C9D4E8",
                opacity: interpolate(frame, [(2.2 + i * 0.3) * fps, (2.7 + i * 0.3) * fps], [0, 1], {
                  extrapolateLeft: "clamp",
                  extrapolateRight: "clamp",
                  easing: Easing.bezier(0.16, 1, 0.3, 1),
                }),
              }}
            >
              {t}
            </div>
          ),
        )}
      </div>

      <SubtitleBar text="歌词式字幕逐句高亮，毛玻璃深色大屏。历史只保存在本地，最多两百条。手机也能当外置麦克风和虚拟遥控器。" />
    </SceneShell>
  );
};
