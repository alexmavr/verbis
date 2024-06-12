import React, { useEffect, useRef } from "react";

interface Props {
  isOpen: boolean;
  onConfirm: () => void;
  onClose?: () => void;
  content: string;
  title?: string;
}

const ConfirmationModal: React.FC<Props> = ({
  isOpen,
  onConfirm,
  onClose,
  content,
  title,
}) => {
  const dialogRef = useRef<HTMLDialogElement>(null);

  useEffect(() => {
    const dialog = dialogRef.current;
    if (isOpen) {
      (dialog as any).showModal();
    } else {
      (dialog as any).close();
    }
  }, [isOpen]);

  return (
    <dialog ref={dialogRef} className="modal">
      <div className="modal-box">
        {title && <h3 className="text-lg font-bold">{title}</h3>}
        <p className="py-4">{content}</p>
        <div className="modal-action">
          <form method="dialog">
            {/* if there is a button in form, it will close the modal */}
            <button className="btn" onClick={onClose}>
              Cancel
            </button>
          </form>
          <button className="btn btn-primary" onClick={onConfirm}>
            Confirm
          </button>
        </div>
      </div>
    </dialog>
  );
};

export default ConfirmationModal;
