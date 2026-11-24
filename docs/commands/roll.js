/// <reference path="wazzap.d.ts" />
// A script: run(ctx) decides what to send.
command({
  name: "roll",
  description: "Rolls dice and sends the result",
  options: [
    { type: "number", name: "dice", description: "How many dice (1 if you leave it out)", min: 1, max: 20 },
    { type: "number", name: "sides", description: "Sides of each (6 if you leave it out)", min: 2, max: 1000 },
  ],

  async run(ctx) {
    const { dice, sides } = ctx.args;
    const rolls = Array.from({ length: dice ?? 1 }, () => 1 + Math.floor(Math.random() * (sides ?? 6)));
    let text = "🎲 " + rolls.join(" + ");
    if (rolls.length > 1) text += ` = *${rolls.reduce((a, b) => a + b)}*`;
    await ctx.send(text);
  },
});
