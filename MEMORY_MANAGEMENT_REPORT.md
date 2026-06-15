# Memory Management & Resource Cleanup Report
## ThermoLens Application Analysis

**Date:** 2026-06-15  
**Status:** ⚠️ Several optimization opportunities identified  
**Severity:** Medium

---

## Executive Summary

ThermoLens is a Python application with generally sound memory practices (using `deque` with bounded sizes), but several resource management and threading issues could impact long-running stability:

1. **No graceful shutdown mechanism** for background threads
2. **Unbounded polling loop** without cleanup handler
3. **Missing resource cleanup** for image objects
4. **Global state with no lifecycle management**

---

## Detailed Findings

### 1. ⚠️ **No Graceful Thread Shutdown** (HIGH PRIORITY)

**Location:** Lines 567-583, 544-565  
**Issue:** Both `data_poller()` and `setup_tray()` threads are daemon threads with no shutdown mechanism.

```python
# Current implementation (PROBLEMATIC)
t_poller = threading.Thread(target=data_poller, daemon=True)
t_poller.start()

def data_poller():
    while True:  # <- Infinite loop, no exit condition
        try:
            # ... monitoring code ...
        except Exception as e:
            print(f"Poller error: {e}")
        time.sleep(2)
```

**Impact:**
- ❌ Threads may not clean up properly during shutdown
- ❌ Active database/hardware connections may not close gracefully
- ❌ Potential resource leaks if `PyLibreHardwareMonitor` maintains open handles

**Recommendation:**
```python
import threading

_shutdown_event = threading.Event()

def data_poller():
    global total_energy_kwh
    psutil.cpu_percent(interval=None)
    last_time = time.time()
    
    while not _shutdown_event.is_set():  # Graceful exit
        try:
            # ... existing code ...
            pass
        except Exception as e:
            print(f"Poller error: {e}")
        
        # Use wait instead of sleep for faster shutdown
        _shutdown_event.wait(timeout=2)

# In main shutdown handler:
def on_shutdown():
    _shutdown_event.set()
    t_poller.join(timeout=5)  # Wait max 5 seconds
```

---

### 2. ⚠️ **Unbounded Tray Icon Loop** (MEDIUM PRIORITY)

**Location:** Lines 544-565  
**Issue:** The `setup_tray()` function blocks indefinitely with `icon.run()`

```python
def setup_tray(app_instance):
    # ... menu setup ...
    icon = pystray.Icon("ThermoLens", tray_image, "ThermoLens", menu)
    icon.run()  # <- Blocking call, only exits on icon.stop()
```

**Impact:**
- ⚠️ Thread waits indefinitely for tray icon interaction
- ⚠️ No timeout or graceful shutdown if tray fails

**Recommendation:**
```python
def setup_tray(app_instance):
    try:
        tray_image = Image.open(resource_path("icon.ico"))
    except:
        tray_image = Image.new('RGBA', (64, 64), (0, 0, 0, 0))
    
    # ... menu setup ...
    icon = pystray.Icon("ThermoLens", tray_image, "ThermoLens", menu)
    
    try:
        icon.run()
    except Exception as e:
        print(f"Tray icon error: {e}")
    finally:
        icon.stop()
        tray_image.close()  # Explicit cleanup
```

---

### 3. ⚠️ **PIL Image Resource Not Closed** (MEDIUM PRIORITY)

**Location:** Lines 559-564  
**Issue:** Image objects are opened but never explicitly closed.

```python
try:
    tray_image = Image.open(resource_path("icon.ico"))
except:
    tray_image = Image.new('RGBA', (64, 64), (0, 0, 0, 0))

icon = pystray.Icon("ThermoLens", tray_image, "ThermoLens", menu)
icon.run()  # <- tray_image is never explicitly closed
```

**Impact:**
- 💾 Open file handle persists for entire application lifetime
- 📊 On Windows, this can prevent file updates/deletion

**Recommendation:**
```python
try:
    with Image.open(resource_path("icon.ico")) as tray_image:
        # Create icon with image copy to avoid close conflicts
        icon = pystray.Icon("ThermoLens", tray_image.copy(), "ThermoLens", menu)
        icon.run()
except Exception as e:
    print(f"Failed to load icon: {e}")
    fallback_image = Image.new('RGBA', (64, 64), (0, 0, 0, 0))
    icon = pystray.Icon("ThermoLens", fallback_image, "ThermoLens", menu)
    icon.run()
```

---

### 4. ⚠️ **Global Mutable State Without Lifecycle** (MEDIUM PRIORITY)

**Location:** Lines 167-181  
**Issue:** Global collections (`cpu_history`, `gpu_history`, etc.) persist without cleanup.

```python
MAX_POINTS = 900  # 30 Minutes
cpu_history = deque([0]*MAX_POINTS, maxlen=MAX_POINTS)  # ✅ Good: bounded
gpu_history = deque([0]*MAX_POINTS, maxlen=MAX_POINTS)  # ✅ Good: bounded
power_history = deque([0]*MAX_POINTS, maxlen=MAX_POINTS)  # ✅ Good: bounded
total_energy_kwh = 0.0  # ⚠️ Unbounded float accumulation
```

**Impact:**
- ✅ History deques are well-bounded (good!)
- ⚠️ `total_energy_kwh` accumulates forever (precision loss over time)
- ⚠️ No serialization/save mechanism for energy data on shutdown

**Recommendation:**
```python
# Use a class to encapsulate state
class MonitoringState:
    def __init__(self):
        self.cpu_history = deque([0]*MAX_POINTS, maxlen=MAX_POINTS)
        self.gpu_history = deque([0]*MAX_POINTS, maxlen=MAX_POINTS)
        self.power_history = deque([0]*MAX_POINTS, maxlen=MAX_POINTS)
        self.total_energy_kwh = 0.0
        self._lock = threading.Lock()
    
    def save_energy(self, filepath):
        with self._lock:
            with open(filepath, 'w') as f:
                json.dump({'total_energy_kwh': self.total_energy_kwh}, f)
    
    def load_energy(self, filepath):
        if os.path.exists(filepath):
            try:
                with open(filepath, 'r') as f:
                    data = json.load(f)
                    self.total_energy_kwh = data.get('total_energy_kwh', 0.0)
            except:
                pass

# Usage
state = MonitoringState()

# On shutdown:
state.save_energy(os.path.expanduser('~/.thermolens/energy.json'))
```

---

### 5. ⚠️ **Exception Suppression in Critical Paths** (MEDIUM PRIORITY)

**Location:** Lines 191-195, 560-562  
**Issue:** Bare `except:` blocks hide critical errors.

```python
try:
    _HM._update_monitor()
except:  # <- Silently ignores ALL exceptions
    pass

try:
    tray_image = Image.open(resource_path("icon.ico"))
except:  # <- Silently fails on any error
    tray_image = Image.new('RGBA', (64, 64), (0, 0, 0, 0))
```

**Impact:**
- 🔴 Hardware monitor failures are hidden
- 🔴 Missing icon silently falls back without logging

**Recommendation:**
```python
try:
    _HM._update_monitor()
except Exception as e:
    # Only suppress expected errors
    if not isinstance(e, (AttributeError, ConnectionError)):
        print(f"Warning: Hardware monitor update failed: {e}")

try:
    tray_image = Image.open(resource_path("icon.ico"))
except FileNotFoundError:
    print("Warning: icon.ico not found, using fallback")
    tray_image = Image.new('RGBA', (64, 64), (0, 0, 0, 0))
except Exception as e:
    print(f"Error loading icon: {e}")
    tray_image = Image.new('RGBA', (64, 64), (0, 0, 0, 0))
```

---

### 6. ✅ **Good Practices Identified**

| Practice | Location | Status |
|----------|----------|--------|
| Bounded deques for history | Lines 167-170 | ✅ Excellent |
| Daemon threads | Lines 569-577 | ✅ Good |
| Lazy hardware monitor loading | Lines 59-66 | ✅ Good |
| Singleton hardware monitor | Line 61 | ✅ Good |
| Exception handling in data poller | Lines 225-226 | ✅ Good |

---

## Memory Footprint Analysis

### Current Allocation (Bounded)
```
cpu_history:     900 floats × 8 bytes = 7.2 KB
gpu_history:     900 floats × 8 bytes = 7.2 KB
power_history:   900 floats × 8 bytes = 7.2 KB
total_energy_kwh: 1 float × 8 bytes   = 8 bytes
UI widgets:      ~2-5 MB (tkinter)
─────────────────────────────────────────
Total: ~2-5 MB (stable, bounded growth)
```

### Long-Running Concerns
- ⚠️ No garbage collection of closed windows
- ⚠️ Tkinter canvas objects accumulate in memory if `draw_chart()` is called frequently
- ⚠️ Global `current_stats` dict updated every 2 seconds (52,560 updates/day)

---

## Recommendations (Priority Order)

| Priority | Issue | Fix Time | Impact |
|----------|-------|----------|--------|
| 🔴 **HIGH** | No graceful thread shutdown | 30 min | Prevents resource leaks |
| 🟠 **MEDIUM** | PIL Image resource leak | 15 min | File handle exhaustion |
| 🟠 **MEDIUM** | Energy data loss on exit | 20 min | Data persistence |
| 🟠 **MEDIUM** | Silent exception handling | 20 min | Debugging difficulty |
| 🟡 **LOW** | Tray icon blocking call | 10 min | Startup delay |

---

## Testing Recommendations

```python
# Test 1: Verify thread cleanup
# - Start app
# - Close after 5 minutes
# - Check: No threads still running (use psutil)

# Test 2: Check file handles
import psutil
p = psutil.Process()
print(p.open_files())  # Should be minimal after startup

# Test 3: Monitor memory over time
# Run for 24 hours and verify no growth beyond 5 MB

# Test 4: Energy persistence
# - Run app, generate power readings
# - Force restart
# - Verify energy values are restored
```

---

## Conclusion

ThermoLens demonstrates good practices in using bounded collections and lazy loading, but lacks proper lifecycle management for threads and resources. Implementing the recommended fixes will significantly improve stability for long-running monitoring sessions.

**Estimated effort to fix all issues: 2-3 hours**

