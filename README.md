# vrunq

### Note
The following Mermaid sequence charts at `Mechanisms` may be updated to reflect the latest commit.

### Mechanisms

1. `Run(ctx context.Context)`
- Spawns ticker goroutine (emits `StatusTick`).
- Spawns a loop in its goroutine.
- Drains `statusCh`, calls `handleEvent`, then cleans up CSV and returns.
```mermaid
sequenceDiagram
    autonumber
    participant Main
    participant Scheduler
    participant TickerGoroutine
    participant LoopGoroutine
    participant StatusCh as statusCh
    participant Handler as handleEvent

    Main->>Scheduler: Run(ctx)
    activate Scheduler

    Scheduler->>TickerGoroutine: start ticker goroutine
    activate TickerGoroutine
    TickerGoroutine-->>StatusCh: emit StatusTick on each tick

    Scheduler->>LoopGoroutine: start loop goroutine
    activate LoopGoroutine
    LoopGoroutine-->>StatusCh: emit StatusDispatch, Preempt/Finish events

    loop Consume events
      StatusCh-->>Handler: send StatusEvent
      activate Handler
      Handler-->>Handler: format & print (+ CSV write)
      deactivate Handler
    end

    LoopGoroutine--xScheduler: ctx.Done → exit loop
    deactivate LoopGoroutine
    Scheduler--xStatusCh: close(statusCh)
    deactivate Scheduler
```

2. `Add (t *Task)`
- Locks, checks duplicates, seeds `Vruntime` to `minVruntime`, inserts into the red-black tree and maps, unlocks, emits `StatusEnqueue` event data.
```mermaid
sequenceDiagram
    autonumber
    participant Client
    participant Scheduler
    participant Mutex
    participant RBT
    participant TasksMap
    participant RanTotals
    participant StatusCh

    Client->>Scheduler: Add(t)
    activate Scheduler

    note right of Scheduler: Lock for RBT data consistency
    Scheduler->>Mutex: Lock()
    note right of Scheduler: Check duplicate
    Scheduler->>TasksMap: lookup t.ID
    alt duplicate exists
        Scheduler->>Mutex: Unlock()
        Scheduler-->>Client: return error
    else new task
        Scheduler->>Scheduler: t.Vruntime = s.minVruntime
        Scheduler->>RBT: Put(nodeKey{t.Vruntime, t.ID}, t)
        Scheduler->>TasksMap: tasks[t.ID] = t
        Scheduler->>RanTotals: ranTotals[t.ID] = 0
        Scheduler->>Scheduler: prepare StatusEvent(StatusEnqueue)
        note right of Scheduler: Lock for RBT data consistency
        Scheduler->>Mutex: Unlock()
        note right of Scheduler: unlock before send to avoid deadlock
        Scheduler->>StatusCh: send StatusEnqueue event
        Scheduler-->>Client: return nil
    end

    deactivate Scheduler
```

3. `AdjustPriority(id TaskID, newPriority int)`
- Adjust the priority(weight => vruntime factor) of the specific task in the red-black tree runqueue.
- The task must be present in the red-black tree, so task in execution will not be able to adjust its task scheduling priority.
```mermaid
sequenceDiagram
    autonumber
    participant Client
    participant Scheduler
    participant Mutex
    participant TasksMap
    participant RBT
    participant StatusCh

    Client->>Scheduler: AdjustPriority(id, newPriority)
    activate Scheduler

    note right of Scheduler: Lock for data consistency
    Scheduler->>Mutex: Lock()

    Scheduler->>TasksMap: lookup t := s.tasks[id]
    alt task not found
        Scheduler->>Mutex: Unlock()
        Scheduler-->>Client: return error
    else task found
        Scheduler->>Scheduler: clamp newPriority to valid range
        Scheduler->>RBT: Remove(nodeKey{t.Vruntime, t.ID})
        note right of Scheduler: Adjust fetched task's metadata
        Scheduler->>Scheduler: set t.Priority & t.Weight
        Scheduler->>RBT: Put(nodeKey{t.Vruntime, t.ID}, t)
        Scheduler->>Mutex: Unlock()
        note right of Scheduler: channel data can be sent after locking
        Scheduler->>StatusCh: send StatusPriorityUpdate event
        Scheduler-->>Client: return nil
    end

    deactivate Scheduler
```

4. `loop(ctx context.Context)`
```mermaid
sequenceDiagram
    autonumber
    participant LoopGoroutine as loop()
    participant Mutex
    participant RBT
    participant Task
    participant StatusCh
    participant Sleep

    loop every iteration
        note right of LoopGoroutine: defer s.statusCh closure
        LoopGoroutine->>LoopGoroutine: if ctx.Err() != nil
        alt context canceled
            LoopGoroutine->>StatusCh: close(statusCh) [deferred]
            LoopGoroutine-->>LoopGoroutine: return
        end

        LoopGoroutine->>Mutex: Lock()
        Mutex-->>LoopGoroutine: locked

        LoopGoroutine->>RBT: Left()
        alt no tasks
            LoopGoroutine->>Mutex: Unlock()
            LoopGoroutine->>Sleep: Sleep(tickDuration)
            note right of LoopGoroutine: Wait until next polling
            LoopGoroutine-->>LoopGoroutine: continue
        else pick next
            LoopGoroutine->>RBT: Remove(nodeKey{min}) (=> Remove least vruntime task)
            LoopGoroutine->>LoopGoroutine: minVruntime = key.vruntime
            LoopGoroutine->>Mutex: Unlock()

            LoopGoroutine->>StatusCh: StatusDispatch(t.ID, t.Vruntime)

            note right of LoopGoroutine: Actually run the selected task
            LoopGoroutine->>Task: Run(ctx with timeout)
            Task-->>LoopGoroutine: err/nil

            LoopGoroutine->>LoopGoroutine: ranTicks = max(1, elapsed/tickDuration)

            note right of LoopGoroutine: Lock again to update the task state
            LoopGoroutine->>Mutex: Lock()
            LoopGoroutine->>LoopGoroutine: calculate new Vruntime 
            LoopGoroutine->>LoopGoroutine: Increment the dispatched task's vruntime

            alt task finished (err == nil)
                LoopGoroutine->>LoopGoroutine: delete(s.tasks[t.ID])
                Note right of LoopGoroutine: kind = StatusFinish
            else preempted (else)
                LoopGoroutine->>RBT: Put(nodeKey{updated}, t)
                Note right of LoopGoroutine: kind = StatusPreempt
            end

            LoopGoroutine->>LoopGoroutine: Find minimum vruntmie in RBT again for next

            LoopGoroutine->>Mutex: Unlock()

            LoopGoroutine->>StatusCh: emit final event(kind, ranTicks, t.Vruntime)
        end
    end
```

5. `handleEvent(ev StatusEvent)`
```mermaid
sequenceDiagram
    autonumber
    participant StatusCh
    participant handleEvent
    participant TickCounter as tickCount
    participant Stdout
    participant CSVWriter

    StatusCh->>handleEvent: deliver ev
    handleEvent->>handleEvent: switch on ev.Kind
    alt ev.Kind == StatusTick
        note right of handleEvent: A new time ticked 
        handleEvent->>TickCounter: tickCount++
        handleEvent-->>StatusCh: return (no I/O)
    else ev.Kind ∈ {StatusPreempt, StatusFinish, …}
        handleEvent->>handleEvent: build center(fmt) helper
        handleEvent->>Stdout: fmt.Println(formatted msg)
        alt csvWriter enabled
            handleEvent->>CSVWriter: Write(rec)
            handleEvent->>CSVWriter: Flush()
        end
    end
```
