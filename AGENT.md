You are a senior software engineer and game systems designer. Your task is to port the original 1978 Oregon Trail BASIC code from https://github.com/clintmoyer/oregon-trail
 into Go (Golang) with full multiplayer functionality.

Requirements:

Faithful Port: Translate all original BASIC game logic into Go, preserving mechanics, randomness, events, text interface, and player decisions.

Multiplayer Server: Create a self-hosted server that allows friends to log in and play together.

Party Management and Turns:

Each player has a party with multiple members (humans or CPU).

Turns rotate only among human players.

CPU party members participate in events automatically but do not consume turns.

After a human player finishes their turn, the next human player in the party takes their turn.

Shared Resources: All players in a session share resources like food, oxen, supplies, and status of party members.

Game Events: Include river crossings, hunting, illnesses, trading, and random events. CPU party members participate automatically where appropriate, but turns always pass to human players.

Client Interface: Terminal-based or minimal GUI. Display:

Current player’s turn and options.

Party status, resources, and overall game state.

Notifications of CPU actions automatically.

Server Features:

Handle multiple human players and sessions.

Persist game state for reconnections.

Handle communication and turn order.

Support basic chat between human players.

Code Requirements: Modular, well-commented, idiomatic Go code. Include data structures, main game loop, server/client communication, event handling, and turn management logic.

Instructions: Provide clear instructions for setting up and running the server and client locally for multiplayer testing.

Enhancements: Suggest optional improvements to enhance multiplayer experience without changing original gameplay mechanics.

Your response should include a complete Go code scaffold demonstrating:

Party and player structures.

Turn rotation logic (human-only rotation, CPU automated).

Server-client setup with connections and state syncing.

Sample implementations of events like hunting, river crossing, and random illness.

How turns pass automatically to the next human player.

Treat this as a blueprint for building a fully functional multiplayer Oregon Trail in Go, suitable for friends to host and play together.