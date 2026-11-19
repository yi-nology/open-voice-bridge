import React from "react";
import { AbsoluteFill, Sequence, staticFile } from "remotion";
import { Audio } from "@remotion/media";
import { TransitionSeries, linearTiming } from "@remotion/transitions";
import { fade } from "@remotion/transitions/fade";
import { SCENES, FPS } from "./durations";
import { Scene1Hook } from "./scenes/Scene1Hook";
import { Scene2Logo } from "./scenes/Scene2Logo";
import { Scene3Flow } from "./scenes/Scene3Flow";
import { Scene4Devices } from "./scenes/Scene4Devices";
import { Scene5Asr } from "./scenes/Scene5Asr";
import { Scene6Subtitle } from "./scenes/Scene6Subtitle";
import { Scene7Security } from "./scenes/Scene7Security";
import { Scene8Outro } from "./scenes/Scene8Outro";

const COMPONENTS = [
  Scene1Hook,
  Scene2Logo,
  Scene3Flow,
  Scene4Devices,
  Scene5Asr,
  Scene6Subtitle,
  Scene7Security,
  Scene8Outro,
];

const TRANSITION_FRAMES = 12;

export const Promo: React.FC = () => {
  return (
    <AbsoluteFill style={{ backgroundColor: "#0A0E17" }}>
      <TransitionSeries>
        {SCENES.map((s, i) => {
          const Comp = COMPONENTS[i];
          return (
            <React.Fragment key={s.id}>
              {i > 0 ? (
                <TransitionSeries.Transition
                  presentation={fade()}
                  timing={linearTiming({ durationInFrames: TRANSITION_FRAMES })}
                />
              ) : null}
              <TransitionSeries.Sequence name={s.id} durationInFrames={s.durationInFrames}>
                <Comp />
                <Sequence from={s.lead} name="voiceover" layout="none">
                  <Audio src={staticFile(s.audio)} />
                </Sequence>
              </TransitionSeries.Sequence>
            </React.Fragment>
          );
        })}
      </TransitionSeries>
    </AbsoluteFill>
  );
};

export const PROMO_FPS = FPS;
