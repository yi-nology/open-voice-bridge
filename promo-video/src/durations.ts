export const FPS = 30;

type SceneMeta = {
  id: string;
  audio: string;
  /** frames of visual lead-in before the voice starts */
  lead: number;
  /** voice duration in seconds */
  voSeconds: number;
  /** frames of tail after the voice ends */
  tail: number;
};

const META: SceneMeta[] = [
  { id: "Scene1Hook", audio: "vo-scene1.mp3", lead: 12, voSeconds: 8.376, tail: 36 },
  { id: "Scene2Logo", audio: "vo-scene2.mp3", lead: 12, voSeconds: 8.544, tail: 36 },
  { id: "Scene3Flow", audio: "vo-scene3.mp3", lead: 12, voSeconds: 10.944, tail: 36 },
  { id: "Scene4Devices", audio: "vo-scene4.mp3", lead: 12, voSeconds: 10.776, tail: 36 },
  { id: "Scene5Asr", audio: "vo-scene5.mp3", lead: 12, voSeconds: 12.696, tail: 36 },
  { id: "Scene6Subtitle", audio: "vo-scene6.mp3", lead: 12, voSeconds: 11.736, tail: 36 },
  { id: "Scene7Security", audio: "vo-scene7.mp3", lead: 12, voSeconds: 12.384, tail: 36 },
  { id: "Scene8Outro", audio: "vo-scene8.mp3", lead: 15, voSeconds: 9.816, tail: 75 },
];

export const SCENES = META.map((m) => ({
  ...m,
  voFrames: Math.ceil(m.voSeconds * FPS),
  durationInFrames: m.lead + Math.ceil(m.voSeconds * FPS) + m.tail,
}));

export const TOTAL_FRAMES = SCENES.reduce((s, x) => s + x.durationInFrames, 0);
