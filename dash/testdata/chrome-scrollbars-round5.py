#!/usr/bin/env python3
import os,sys
chrome='/home/nova/.cache/ms-playwright/chromium_headless_shell-1254/chrome-headless-shell-linux64/chrome-headless-shell'
os.execv(chrome,[chrome]+[arg for arg in sys.argv[1:] if arg!='--hide-scrollbars'])
