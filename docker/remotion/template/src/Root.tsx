import {AbsoluteFill, Composition, interpolate, useCurrentFrame} from 'remotion';

const Hello = () => {
  const frame = useCurrentFrame();
  const opacity = interpolate(frame, [0, 15], [0, 1], {extrapolateRight: 'clamp'});
  return (
    <AbsoluteFill style={{backgroundColor: '#14213d', justifyContent: 'center', alignItems: 'center'}}>
      <div style={{color: '#ffffff', fontFamily: 'Noto Sans, sans-serif', fontSize: 64, opacity}}>
        GoClaw + Remotion
      </div>
      <div style={{color: '#fca311', fontFamily: 'Noto Sans, sans-serif', fontSize: 32, marginTop: 24}}>
        Video đã sẵn sàng
      </div>
    </AbsoluteFill>
  );
};

export const Root = () => (
  <Composition id="Hello" component={Hello} durationInFrames={60} fps={30} width={1280} height={720} />
);
