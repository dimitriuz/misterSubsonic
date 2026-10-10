#include <dlfcn.h>
#include <stddef.h>

#include "shim.h"

/* ---------- hardware volume ----------
   ALSA's simple mixer, through libasound loaded at run time (as miniaudio
   loads it), so the build needs no ALSA headers or libraries. One mixer at
   a time: the card of the native device. */

typedef struct snd_mixer snd_mixer_t;
typedef struct snd_mixer_elem snd_mixer_elem_t;

static struct {
    void* lib;
    int (*open)(snd_mixer_t**, int);
    int (*attach)(snd_mixer_t*, const char*);
    int (*selem_register)(snd_mixer_t*, void*, void**);
    int (*load)(snd_mixer_t*);
    int (*close)(snd_mixer_t*);
    snd_mixer_elem_t* (*first_elem)(snd_mixer_t*);
    snd_mixer_elem_t* (*elem_next)(snd_mixer_elem_t*);
    int (*has_playback_volume)(snd_mixer_elem_t*);
    int (*get_playback_dB_range)(snd_mixer_elem_t*, long*, long*);
    int (*set_playback_dB_all)(snd_mixer_elem_t*, long, int);
    int (*get_playback_dB)(snd_mixer_elem_t*, int, long*);
    int (*has_playback_switch)(snd_mixer_elem_t*);
    int (*set_playback_switch_all)(snd_mixer_elem_t*, int);
} A;

static snd_mixer_t* g_mixer;
static snd_mixer_elem_t* g_elem;

static int load_lib(void) {
    if (A.lib != NULL) {
        return 0;
    }
    void* lib = dlopen("libasound.so.2", RTLD_NOW);
    if (lib == NULL) {
        lib = dlopen("libasound.so", RTLD_NOW);
    }
    if (lib == NULL) {
        return -1;
    }
#define SYM(field, name) \
    if ((*(void**)&A.field = dlsym(lib, name)) == NULL) { dlclose(lib); return -1; }
    SYM(open, "snd_mixer_open")
    SYM(attach, "snd_mixer_attach")
    SYM(selem_register, "snd_mixer_selem_register")
    SYM(load, "snd_mixer_load")
    SYM(close, "snd_mixer_close")
    SYM(first_elem, "snd_mixer_first_elem")
    SYM(elem_next, "snd_mixer_elem_next")
    SYM(has_playback_volume, "snd_mixer_selem_has_playback_volume")
    SYM(get_playback_dB_range, "snd_mixer_selem_get_playback_dB_range")
    SYM(set_playback_dB_all, "snd_mixer_selem_set_playback_dB_all")
    SYM(get_playback_dB, "snd_mixer_selem_get_playback_dB")
    SYM(has_playback_switch, "snd_mixer_selem_has_playback_switch")
    SYM(set_playback_switch_all, "snd_mixer_selem_set_playback_switch_all")
#undef SYM
    A.lib = lib;
    return 0;
}

void mss_mixer_close(void) {
    if (g_mixer != NULL) {
        A.close(g_mixer);
        g_mixer = NULL;
        g_elem = NULL;
    }
}

int mss_mixer_open(const char* card) {
    mss_mixer_close();
    if (load_lib() != 0) {
        return -1;
    }
    snd_mixer_t* m;
    if (A.open(&m, 0) < 0) {
        return -2;
    }
    if (A.attach(m, card) < 0 || A.selem_register(m, NULL, NULL) < 0 || A.load(m) < 0) {
        A.close(m);
        return -3;
    }
    /* The first control with a playback volume in dB. */
    for (snd_mixer_elem_t* e = A.first_elem(m); e != NULL; e = A.elem_next(e)) {
        long lo, hi;
        if (A.has_playback_volume(e) && A.get_playback_dB_range(e, &lo, &hi) == 0 && lo < hi) {
            g_mixer = m;
            g_elem = e;
            return 0;
        }
    }
    A.close(m);
    return -4;
}

int mss_mixer_range(long* min_cdb, long* max_cdb) {
    if (g_elem == NULL) {
        return -1;
    }
    return A.get_playback_dB_range(g_elem, min_cdb, max_cdb);
}

int mss_mixer_set(long cdb, long* got_cdb) {
    if (g_elem == NULL) {
        return -1;
    }
    int r = A.set_playback_dB_all(g_elem, cdb, 1); /* the nearest step at or above */
    if (r < 0) {
        return r;
    }
    return A.get_playback_dB(g_elem, 0, got_cdb);
}

int mss_mixer_has_switch(void) {
    return g_elem != NULL && A.has_playback_switch(g_elem);
}

int mss_mixer_set_switch(int on) {
    if (g_elem == NULL) {
        return -1;
    }
    return A.set_playback_switch_all(g_elem, on);
}

int mss_mixer_get(long* cdb) {
    if (g_elem == NULL) {
        return -1;
    }
    return A.get_playback_dB(g_elem, 0, cdb);
}
