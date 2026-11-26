package wa

// A location request (requestLocationMessage, which holds nothing but its
// context) asks the chat for your location; only phones answer one, with a
// live location. Its sender takes it back with a requestLocationUpdateMessage
// (CANCEL) naming it, kept in the message's opened column. It's a message
// with model.Message.LocationRequest set and no text.
