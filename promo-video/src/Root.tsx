import React from "react";
import { Composition, Folder } from "remotion";
import { Promo } from "./Promo";
import { FPS, SCENES, TOTAL_FRAMES } from "./durations";
import { Scene1Hook } from "./scenes/Scene1Hook";
import { Scene2Logo } from "./scenes/Scene2Logo";
import { Scene3Flow } from "./scenes/Scene3Flow";
import { Scene4Devices } from "./scenes/Scene4Devices";
import { Scene5Asr } from "./scenes/Scene5Asr";
import { Scene6Subtitle } from "./scenes/Scene6Subtitle";
import { Scene7Security } from "./scenes/Scene7Security";
import { Scene8Outro } from "./scenes/Scene8Outro";

const SCENE_COMPONENTS = [
  Scene1Hook,
  Scene2Logo,
  Scene3Flow,
  Scene4Devices,
  Scene5Asr,
  Scene6Subtitle,
  Scene7Security,
  Scene8Outro,
];

export const RemotionRoot: React.FC = () => {
  return (
    <>
      <Composition
        id="OpenVoiceBridgePromo"
        component={Promo}
        durationInFrames={TOTAL_FRAMES - 12 * (SCENES.length - 1)}
        fps={FPS}
        width={1920}
        height={1080}
      />
      <Folder name="Scenes">
        {SCENES.map((s, i) => (
          <Composition
            key={s.id}
            id={s.id}
            component={SCENE_COMPONENTS[i]}
            durationInFrames={s.durationInFrames}
            fps={FPS}
            width={1920}
            height={1080}
          />
        ))}
      </Folder>
    </>
  );
};
