category: Fixed
- **FIT upload works from mobile browsers** - Removed the filename extension check from the upload handler. Mobile browsers (especially iOS) may omit or alter the filename when sharing .fit files from workout apps; the file format is now validated by the FIT parser instead of the filename. (Hytte-al5x)
