/// <reference path="wazzap.d.ts" />
// Sends the message you reply to again, whatever it is: its raw payload.
command({
  name: "again",
  description: "Sends the message you reply to again, as many times as you say",
  options: [{ type: "number", name: "times", description: "How many times (1 if you leave it out)", min: 1, max: 5 }],

  async run(ctx) {
    if (!ctx.quoted) {
      await ctx.note("Reply to a message with /again.");
      return;
    }
    const payload = await ctx.sock.getMessage(ctx.quoted);
    for (let i = 0; i < (ctx.args.times ?? 1); i++) {
      if (i > 0) await sleep(1000);
      await ctx.sock.relayMessage(ctx.chat.id, payload);
    }
  },
});
