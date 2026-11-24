// What a command's script has. Put this file beside your command and start
// the command with
//   /// <reference path="wazzap.d.ts" />
// and an editor like VS Code completes it and shows what each thing takes.

/** Declares the file's command. Give it either `text` or `run`. */
declare function command(spec: CommandSpec): void;

/** Waits a while: `await sleep(1000)`. */
declare function sleep(ms: number): Promise<void>;
declare function setTimeout(f: (...args: any[]) => void, ms?: number, ...args: any[]): number;
declare function clearTimeout(id: number): void;
/** What it's given shows in a note only you see, once the command ends. */
declare var console: Console;
interface Console {
  log(...values: any[]): void;
  info(...values: any[]): void;
  debug(...values: any[]): void;
  warn(...values: any[]): void;
  error(...values: any[]): void;
}

interface CommandSpec {
  /** Lowercase letters, digits and _, at most 24. */
  name: string;
  description: string;
  /** The required ones first; a text option last. */
  options?: Option[];
  /** A template to send. {option} is filled in, and {chat}, {date}... as in snippets. */
  text?: string;
  run?(ctx: Ctx): void | Promise<void>;
  /** Only in groups. */
  group?: boolean;
  /** Only where you're an admin. */
  admin?: boolean;
}

type Option = { name: string; description?: string; required?: boolean } & (
  | { type: "text" }
  /** Whole numbers; min is 0 unless you give it. */
  | { type: "number"; min?: number; max?: number }
  /** 1 to 25 words without spaces. */
  | { type: "choice"; choices: string[] }
  | { type: "member"; multiple?: boolean }
);

interface Ctx {
  /** The options' values by name: a string, a number, a Member (or a list of them); null, or [] with multiple, when left out. */
  args: Record<string, any>;
  chat: { id: string; name: string; group: boolean };
  /** The message you reply to. */
  quoted: Message | null;
  /** Sends a message in this chat, as a reply if you were replying. At most 4096 bytes. */
  send(text: string | number): Promise<Message>;
  /** The group's members. */
  members(): Promise<Member[]>;
  /** Puts text on the clipboard. */
  copy(text: string | number): Promise<null>;
  /** Shows a note only you see. */
  note(text: string | number): Promise<null>;
  /** Shows a note with 1 to 5 buttons, and gives the one clicked; null when dismissed. */
  ask(text: string, buttons: string[]): Promise<string | null>;
  sock: Sock;
}

interface Member {
  id: string;
  name: string;
  /** Put it in a message's text to mention them. */
  mention: string;
  admin: "admin" | "superadmin" | null;
  me: boolean;
}

interface Key {
  remoteJid: string;
  id: string;
  fromMe?: boolean;
  participant?: string;
}

interface Message {
  key: Key;
  text: string;
  /** The sender's name. */
  sender: string;
  senderId: string;
  fromMe: boolean;
  /** Unix seconds. */
  time: number;
  media: "" | "image" | "video" | "gif" | "ptt" | "audio" | "document" | "sticker" | "location" | "contact" | "poll" | "event" | "stickerPack";
  fileName: string;
}

/** A file you picked. Scripts can only send it. */
interface PickedFile {
  readonly name: string;
}

type MessageOrKey = Message | Key;

type Content =
  | { text: string; mentions?: string[]; viewOnce?: boolean }
  | { image: PickedFile; caption?: string; mentions?: string[]; viewOnce?: boolean }
  | { video: PickedFile; caption?: string; mentions?: string[]; viewOnce?: boolean; gifPlayback?: boolean }
  | { audio: PickedFile }
  | { document: PickedFile; caption?: string; mentions?: string[] }
  /** A sticker's message. */
  | { sticker: MessageOrKey }
  /** Empty text takes it back. */
  | { react: { text: string; key: MessageOrKey } }
  | { edit: MessageOrKey; text: string; mentions?: string[] }
  /** For everyone. */
  | { delete: MessageOrKey }
  | { forward: MessageOrKey }
  /** 2 to 12 values; one answer only if selectableCount is 1. */
  | { poll: { name: string; values: string[]; selectableCount?: number } }
  | { location: { degreesLatitude: number; degreesLongitude: number; name?: string; address?: string } }
  /** Jids. */
  | { contacts: string[] }
  /** `type` 1 pins and 2 unpins; `time` is in seconds, 7 days unless you give it. */
  | { pin: MessageOrKey; type?: 1 | 2; time?: number };

/** The whole account, named after Baileys' sock. */
interface Sock {
  user: { id: string; lid: string; name: string } | null;
  /**
   * Resolves once the message is queued, so one just sent can't be edited yet.
   * react, edit, delete and pin give the message they were done to.
   */
  sendMessage(jid: string, content: Content, options?: { quoted?: MessageOrKey }): Promise<Message>;
  /** Sends any protobuf message (waE2E.Message). Unlike Baileys', gives a message: its ID is `.key.id`. */
  relayMessage(jid: string, message: object, options?: { quoted?: MessageOrKey; messageId?: string }): Promise<Message>;
  /** A message's raw protobuf as JSON: enums are names, 64-bit numbers strings and bytes base64. */
  getMessage(message: MessageOrKey): Promise<any>;
  /** A chat's newest stored messages (20, at most 500), oldest first. */
  fetchMessages(jid: string, limit?: number): Promise<Message[]>;
  chats(): Promise<{ id: string; name: string; group: boolean; unread: number; archived: boolean; pinned: boolean; muted: boolean }[]>;
  contacts(): Promise<{ id: string; name: string; phone: string }[]>;
  onWhatsApp(...phones: string[]): Promise<{ jid: string | null; exists: boolean; name: string }[]>;
  /** Asks you for a file to send. */
  pickFile(title?: string): Promise<PickedFile | null>;
  /** Marks the chats these are in as read. */
  readMessages(keys: (MessageOrKey | string)[]): Promise<null>;
  /** mute is in milliseconds, null to unmute. */
  chatModify(
    changes: { archive?: boolean; pin?: boolean; markRead?: boolean; mute?: number | null; star?: { messages: { id: string }[]; star: boolean } },
    jid: string,
  ): Promise<null>;
  star(jid: string, messages: (MessageOrKey | { id: string })[], star: boolean): Promise<null>;
  updateBlockStatus(jid: string, action: "block" | "unblock"): Promise<null>;
  groupMetadata(jid: string): Promise<{
    id: string;
    subject: string;
    desc: string;
    announce: boolean;
    restrict: boolean;
    /** Unix seconds. */
    creation: number;
    size: number;
    participants: Member[];
  }>;
  /** `error` says why it failed for someone, or is "". */
  groupParticipantsUpdate(
    jid: string,
    participants: (string | { id: string })[],
    action: "add" | "remove" | "promote" | "demote",
  ): Promise<{ jid: string; name: string; status: "200" | "error"; error: string }[]>;
  groupSettingUpdate(jid: string, setting: "announcement" | "not_announcement" | "locked" | "unlocked"): Promise<null>;
  groupUpdateSubject(jid: string, subject: string): Promise<null>;
  groupUpdateDescription(jid: string, description?: string): Promise<null>;
  groupInviteCode(jid: string): Promise<string>;
  groupRevokeInvite(jid: string): Promise<string>;
  groupLeave(jid: string): Promise<null>;
}
