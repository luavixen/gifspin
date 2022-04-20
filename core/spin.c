#include <vips/vips.h>

gint main(gint argc, gchar **argv) {
    gint opt_width;
    gint opt_height;
    gint opt_frame_count;
    gint opt_frame_delay;
    gboolean opt_flag_crop;
    gboolean opt_flag_reverse;
    gboolean opt_flag_flatten;
    gint64 opt_background;
    gchar *opt_path_input;
    gchar *opt_path_output;

    if (argc != 11) {
        g_printerr("not enough arguments, expected 10 but got %d\n", argc - 1);
        g_printerr("supply arguments in the following form:\n");
        g_printerr("[width] [height] [frame_count] [frame_delay] [flag_crop] [flag_reverse] [flag_flatten] [background] [input] [output]\n");
        return EXIT_FAILURE;
    }

    opt_width        = (gint) g_ascii_strtoll(argv[1], NULL, 0);
    opt_height       = (gint) g_ascii_strtoll(argv[2], NULL, 0);
    opt_frame_count  = (gint) g_ascii_strtoll(argv[3], NULL, 0);
    opt_frame_delay  = (gint) g_ascii_strtoll(argv[4], NULL, 0);
    opt_flag_crop    = 0 != g_ascii_strtoll(argv[5], NULL, 0);
    opt_flag_reverse = 0 != g_ascii_strtoll(argv[6], NULL, 0);
    opt_flag_flatten = 0 != g_ascii_strtoll(argv[7], NULL, 0);
    opt_background   = g_ascii_strtoll(argv[8], NULL, 0);
    opt_path_input   = g_strdup(argv[9]);
    opt_path_output  = g_strdup(argv[10]);

    if (opt_width < 4 || opt_width > 65535 || opt_height < 4 || opt_height > 65535) {
        g_printerr("image dimensions out of range (width %d, height %d)\n", opt_width, opt_height);
        return EXIT_FAILURE;
    }
    if (opt_frame_count < 1 || opt_frame_count > 2048) {
        g_printerr("frame count out of range (%d)\n", opt_frame_count);
        return EXIT_FAILURE;
    }
    if (opt_frame_delay < 1 || opt_frame_delay > 600000) {
        g_printerr("frame delay out of range (%d)\n", opt_frame_delay);
        return EXIT_FAILURE;
    }

    if (VIPS_INIT(argv[0])) {
        g_printerr("vips init: %s", vips_error_buffer());
        return EXIT_FAILURE;
    }

    VipsImage *source = vips_image_new_from_file(opt_path_input, NULL);

    if (source == NULL) {
        g_printerr("load source: %s", vips_error_buffer());
        return EXIT_FAILURE;
    }

    gchar **source_fields = vips_image_get_fields(source);
    gchar *source_field;

    for (gsize i = 0; (source_field = source_fields[i]) != NULL; i++) {
        vips_image_remove(source, source_field);
    }

    g_strfreev(source_fields);

    if (vips_image_get_interpretation(source) != VIPS_INTERPRETATION_sRGB) {
        VipsImage *source_old = source;

        if (vips_colourspace(source_old, &source, VIPS_INTERPRETATION_sRGB, NULL)) {
            g_printerr("colourspace source: %s", vips_error_buffer());
            return EXIT_FAILURE;
        }

        g_object_unref(source_old);
    }

    gdouble background_array[] = {
        (gdouble) (opt_background >> 24 & 0xFF),
        (gdouble) (opt_background >> 16 & 0xFF),
        (gdouble) (opt_background >>  8 & 0xFF),
        (gdouble) (opt_background       & 0xFF),
    };

    VipsArrayDouble *background_4 = vips_array_double_new(background_array, 4);
    VipsArrayDouble *background_3 = vips_array_double_new(background_array, 3);

    if (vips_image_hasalpha(source)) {
        VipsImage *source_old = source;

        if (opt_flag_flatten) {
            if (vips_flatten(source_old, &source, "background", background_3, NULL)) {
                g_printerr("flatten source: %s", vips_error_buffer());
                return EXIT_FAILURE;
            }
        }
        else {
            if (vips_premultiply(source_old, &source, NULL)) {
                g_printerr("premultiply source: %s", vips_error_buffer());
                return EXIT_FAILURE;
            }
        }

        g_object_unref(source_old);
    }
    else if (!opt_flag_crop) {
        VipsImage *source_old = source;

        if (vips_addalpha(source_old, &source, NULL)) {
            g_printerr("addalpha source: %s", vips_error_buffer());
            return EXIT_FAILURE;
        }

        g_object_unref(source_old);
    }

    VipsArrayDouble *background = (VipsArrayDouble *) vips_area_copy(VIPS_AREA(vips_image_hasalpha(source) ? background_4 : background_3));
    vips_area_unref(VIPS_AREA(background_4));
    vips_area_unref(VIPS_AREA(background_3));

    gint source_real_width = vips_image_get_width(source);
    gint source_real_height = vips_image_get_height(source);
    gint source_width = opt_width;
    gint source_height = opt_height;

    if (source_real_width != source_width) {
        gdouble scale =
            ((gdouble) source_width) /
            ((gdouble) source_real_width);

        VipsImage *source_old = source;

        if (vips_resize(source_old, &source, scale, "kernel", VIPS_KERNEL_CUBIC, NULL)) {
            g_printerr("resize source: %s", vips_error_buffer());
            return EXIT_FAILURE;
        }

        g_object_unref(source_old);

        source_real_width = vips_image_get_width(source);
        source_real_height = vips_image_get_height(source);
        source_width = source_real_width;
    }

    if (opt_flag_crop) {
        gint source_length = 4096;
        source_length = source_length < source_width ? source_length : source_width;
        source_length = source_length < source_height ? source_length : source_height;
        source_length = source_length < source_real_width ? source_length : source_real_width;
        source_length = source_length < source_real_height ? source_length : source_real_height;

        if (source_real_width != source_length || source_real_height != source_length) {
            VipsImage *source_old = source;

            if (vips_smartcrop(
                source_old, &source,
                source_length, source_length,
                "interesting", VIPS_INTERESTING_CENTRE,
                NULL
            )) {
                g_printerr("smartcrop (square) source: %s", vips_error_buffer());
                return EXIT_FAILURE;
            }

            g_object_unref(source_old);
        }

        source_real_width = source_length;
        source_real_height = source_length;
        source_width = source_length;
        source_height = source_length;
    }
    else if (source_real_height > source_height || source_real_width > source_width) {
        VipsImage *source_old = source;

        source_width = source_width < source_real_width ? source_width : source_real_width;
        source_height = source_height < source_real_height ? source_real_height : source_real_height;

        if (vips_smartcrop(
            source_old, &source,
            source_width, source_height,
            "interesting", VIPS_INTERESTING_CENTRE,
            NULL
        )) {
            g_printerr("smartcrop (height) source: %s", vips_error_buffer());
            return EXIT_FAILURE;
        }

        g_object_unref(source_old);

        source_real_width = source_width;
        source_real_height = source_height;
    }
    else {
        if (source_width != source_real_width) {
            source_width = source_real_width;
        }
        if (source_height != source_real_height) {
            source_height = source_real_height;
        }
    }

    gsize frames_length = (gsize) opt_frame_count;
    VipsImage **frames = g_malloc_n(frames_length, sizeof(VipsImage *));

    gint frame_area_x;
    gint frame_area_y;
    gint frame_area_width;
    gint frame_area_height;

    if (opt_flag_crop) {
        // diagram explaining this constant: https://i.imgur.com/jmGN2wO.png
        gint frame_area_begin = (gint) ceil(0.14644660940672627 * (gdouble) source_width);
        gint frame_area_end = source_width - frame_area_begin * 2;
        frame_area_x = frame_area_begin;
        frame_area_y = frame_area_begin;
        frame_area_width = frame_area_end;
        frame_area_height = frame_area_end;
    }
    else if (!opt_flag_flatten) {
        frame_area_x = -1;
        frame_area_y = -1;
        frame_area_width = source_width + 2;
        frame_area_height = source_height + 2;
    }
    else {
        frame_area_x = 0;
        frame_area_y = 0;
        frame_area_width = source_width;
        frame_area_height = source_height;
    }

    VipsArrayInt *frame_area = vips_array_int_newv(
        4,
        frame_area_x,
        frame_area_y,
        frame_area_width,
        frame_area_height
    );

    gdouble frame_odx = 0.5 * (gdouble) source_width;
    gdouble frame_ody = 0.5 * (gdouble) source_height;
    gdouble frame_idx = -frame_odx;
    gdouble frame_idy = -frame_ody;

    gdouble frame_angle_interval = 6.283185307179586 / (gdouble) frames_length;

    if (opt_flag_reverse) {
        frame_angle_interval = -frame_angle_interval;
    }

    for (gsize i = 0; i < frames_length; i++) {
        gdouble angle = frame_angle_interval * (gdouble) i;

        gdouble a = +cos(angle);
        gdouble b = -sin(angle);
        gdouble c = -b;
        gdouble d = +a;

        VipsImage *frame;

        if (vips_affine(
            source, &frame,
            a, b, c, d,
            "idx", frame_idx,
            "idy", frame_idy,
            "odx", frame_odx,
            "ody", frame_ody,
            "oarea", frame_area,
            "extend", VIPS_EXTEND_BACKGROUND,
            "background", background,
            NULL
        )) {
            g_printerr("affine frame %d: %s", (gint) i, vips_error_buffer());
            return EXIT_FAILURE;
        }

        if (source_width != frame_area_width || source_height != frame_area_height) {
            VipsImage *frame_old = frame;

            if (vips_copy(
                frame_old, &frame,
                "xres", (gdouble) source_width,
                "yres", (gdouble) source_height,
                NULL
            )) {
                g_printerr("copy frame %d: %s", (gint) i, vips_error_buffer());
                return EXIT_FAILURE;
            }

            g_object_unref(frame_old);
        }

        frames[i] = frame;
    }

    vips_area_unref(VIPS_AREA(background));
    vips_area_unref(VIPS_AREA(frame_area));
    g_object_unref(source);

    source_width = frame_area_width;
    source_height = frame_area_height;

    VipsImage *target;

    if (vips_arrayjoin(frames, &target, (gint) frames_length, "across", 1, NULL)) {
        g_printerr("arrayjoin target: %s", vips_error_buffer());
        return EXIT_FAILURE;
    }

    for (gsize i = 0; i < frames_length; i++) {
        g_object_unref(frames[i]);
    }
    g_free(frames);

    gint *frames_delays = g_malloc_n(frames_length, sizeof(gint));
    for (gsize i = 0; i < frames_length; i++) {
        frames_delays[i] = opt_frame_delay;
    }

    vips_image_set_array_int(target, "delay", frames_delays, (gint) frames_length);
    vips_image_set_int(target, "page-height", source_height);

    if (vips_gifsave(target, opt_path_output, NULL)) {
        g_printerr("gifsave target: %s", vips_error_buffer());
        return EXIT_FAILURE;
    }

    g_object_unref(target);
    g_free(frames_delays);

    vips_shutdown();

    g_free(opt_path_input);
    g_free(opt_path_output);

    return EXIT_SUCCESS;
}
