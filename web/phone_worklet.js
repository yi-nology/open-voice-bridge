// Captures microphone audio and posts it to the main thread in ~50 ms blocks.
// No resampling here: keeping one block of Float32 per message avoids
// per-quantum postMessage overhead while leaving sample-rate handling to
// phone_app.js, where the phase accumulator is easy to test and reason about.
class MicPump extends AudioWorkletProcessor {
  constructor(options) {
    super(options);
    const inputRate = options.processorOptions && options.processorOptions.inputRate
      ? options.processorOptions.inputRate
      : sampleRate;
    // A whole number of 128-sample render quanta, so the common case is an
    // exact multiple and no input sample is ever left stranded.
    this.blockSize = Math.max(256, Math.ceil(inputRate / 20 / 128) * 128);
    this.block = new Float32Array(this.blockSize);
    this.filled = 0;
    this.active = true;
    this.port.onmessage = (event) => {
      if (event.data === 'stop') this.active = false;
    };
  }

  process(inputs) {
    if (!this.active) return false;
    const channel = inputs[0] && inputs[0][0];
    if (!channel) return true;
    // Grow instead of clipping: dropping the tail of a quantum would delete
    // audio the speaker actually produced.
    if (this.filled + channel.length > this.block.length) {
      const grown = new Float32Array(Math.max(this.block.length * 2, this.filled + channel.length));
      grown.set(this.block.subarray(0, this.filled));
      this.block = grown;
    }
    this.block.set(channel, this.filled);
    this.filled += channel.length;
    while (this.filled >= this.blockSize) {
      const out = this.block.slice(0, this.blockSize);
      this.port.postMessage(out, [out.buffer]);
      this.block.copyWithin(0, this.blockSize, this.filled);
      this.filled -= this.blockSize;
    }
    return true;
  }
}

registerProcessor('mic-pump', MicPump);
