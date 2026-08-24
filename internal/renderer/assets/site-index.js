/**
 * Site Index JavaScript Module
 * Entry point for the multi-site directory page.
 *
 * Registers only <time-formatter>, which upgrades absolute <time datetime>
 * values into relative times in the reader's timezone. The feed reader's
 * other custom elements are not used on this page.
 */

import './js/time-formatter.js';
