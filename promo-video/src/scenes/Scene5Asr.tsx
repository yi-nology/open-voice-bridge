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

const ENGINES = ["MiMo", "OpenAI 兼容", "whisper.cpp", "本地命令"];

export const Scene5Asr: React.FC = () => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();

  return (
    <SceneShell glowX="50%" glowY="30%">
      <Interactive.Div
        name="Scene title"
        style={{
          position: "absolute",
          top: 110,
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
        识别引擎 <span style={{ color: COLORS.accent }}>可插拔</span>，随时热切换
      </Interactive.Div>

      {/* engine chips around hub */}
      <div
        style={{
          position: "absolute",
          top: 300,
          left: 0,
          right: 0,
          display: "flex",
          justifyContent: "center",
          alignItems: "center",
          gap: 28,
        }}
      >
        {ENGINES.map((e, i) => {
          const t0 = 0.4 + i * 0.3;
          return (
            <Interactive.Div
              key={e}
              name={`Engine ${e}`}
              style={{
                padding: "28px 40px",
                borderRadius: 20,
                fontSize: 38,
                fontWeight: 700,
                letterSpacing: 1,
                background: "rgba(45,212,191,0.1)",
                border: "1.5px solid rgba(45,212,191,0.5)",
                boxShadow: "0 0 36px rgba(45,212,191,0.18)",
                opacity: interpolate(frame, [t0 * fps, (t0 + 0.5) * fps], [0, 1], {
                  extrapolateLeft: "clamp",
                  extrapolateRight: "clamp",
                  easing: Easing.bezier(0.16, 1, 0.3, 1),
                }),
                translate: interpolate(
                  frame,
                  [t0 * fps, (t0 + 0.5) * fps],
                  ["0px 30px", "0px 0px"],
                  {
                    extrapolateLeft: "clamp",
                    extrapolateRight: "clamp",
                    easing: Easing.bezier(0.16, 1, 0.3, 1),
                  },
                ),
              }}
            >
              {e}
            </Interactive.Div>
          );
        })}
      </div>

      {/* fallback chain */}
      <Interactive.Div
        name="Fallback chain"
        style={{
          position: "absolute",
          top: 500,
          width: "100%",
          display: "flex",
          justifyContent: "center",
          alignItems: "center",
          gap: 20,
          opacity: interpolate(frame, [1.8 * fps, 2.4 * fps], [0, 1], {
            extrapolateLeft: "clamp",
            extrapolateRight: "clamp",
            easing: Easing.bezier(0.16, 1, 0.3, 1),
          }),
        }}
      >
        <div
          style={{
            fontSize: 30,
            color: COLORS.muted,
            letterSpacing: 2,
            padding: "14px 26px",
            borderRadius: 999,
            border: "1px dashed rgba(255,255,255,0.22)",
          }}
        >
          fallback 链 · 失败自动切下一个
        </div>
      </Interactive.Div>

      {/* post-processing cards */}
      <div
        style={{
          position: "absolute",
          top: 620,
          left: 140,
          right: 140,
          display: "flex",
          gap: 30,
        }}
      >
        {[
          {
            t: "文本后处理",
            d: "MiniMax · 火山方舟 · DeepSeek · 任意 OpenAI 兼容，失败保留原文",
          },
          {
            t: "个人词典",
            d: "本地纠正专名与品牌，识别结果更贴合你的用词",
          },
          {
            t: "语音短语",
            d: "触发词替换成模板，按一下直接上屏，SayAll 风格",
          },
        ].map((c, i) => {
          const t0 = 2.6 + i * 0.35;
          return (
            <Interactive.Div
              key={c.t}
              name={`Feature ${c.t}`}
              style={{
                flex: 1,
                padding: "32px 34px",
                borderRadius: 22,
                background: "rgba(255,255,255,0.04)",
                border: "1px solid rgba(255,255,255,0.1)",
                opacity: interpolate(frame, [t0 * fps, (t0 + 0.55) * fps], [0, 1], {
                  extrapolateLeft: "clamp",
                  extrapolateRight: "clamp",
                  easing: Easing.bezier(0.16, 1, 0.3, 1),
                }),
                translate: interpolate(
                  frame,
                  [t0 * fps, (t0 + 0.55) * fps],
                  ["0px 32px", "0px 0px"],
                  {
                    extrapolateLeft: "clamp",
                    extrapolateRight: "clamp",
                    easing: Easing.bezier(0.16, 1, 0.3, 1),
                  },
                ),
              }}
            >
              <div style={{ fontSize: 38, fontWeight: 700, letterSpacing: 1 }}>{c.t}</div>
              <div
                style={{
                  fontSize: 27,
                  color: COLORS.muted,
                  marginTop: 14,
                  lineHeight: 1.65,
                  letterSpacing: 1,
                }}
              >
                {c.d}
              </div>
            </Interactive.Div>
          );
        })}
      </div>

      <SubtitleBar text="识别引擎可插拔：MiMo、OpenAI 兼容接口、whisper.cpp、本地命令随意切换，支持 fallback 链。个人词典帮你纠正专名，语音短语一句话直接上屏。" />
    </SceneShell>
  );
};
