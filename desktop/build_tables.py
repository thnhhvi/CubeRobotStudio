"""Pre-generate the Kociemba tables next to the package so the .exe starts instantly."""
import os
import shutil
import tempfile

from cuberobot import twophase

tmp = tempfile.mkdtemp()
twophase.Tables(cache_dir=tmp)
name = f"twophase_v{twophase._TABLE_VERSION}.npz"
dst = os.path.join(os.path.dirname(os.path.abspath(twophase.__file__)), name)
shutil.copy(os.path.join(tmp, name), dst)
print("tables ->", dst)
