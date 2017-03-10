# Writing Mixer Adapters

This will eventually turn into a developer's guide for 
creating mixer adapters. For now, it's just a set of
notes and reminders:

- Adapters must use env.Logger for logging during
execution. This logger understands about which adapter
is running and routes the data to the place where the
operator wants to see it.

- Adapters must use env.ScheduleWork in order to 
dispatch goroutines. This ensures all adapter goroutines
are prevented from crashing the mixer as a whole by catching
any panics they produce.
